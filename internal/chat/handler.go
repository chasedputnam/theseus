package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/google/uuid"
)

// toolIntentPatterns — plain-chat messages matching these escalate to agent loop.
var toolIntentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(remind me|set a reminder|add to calendar|create event|schedule)\b`),
	regexp.MustCompile(`(?i)\b(add a (note|todo|task)|create a (note|todo|task))\b`),
	regexp.MustCompile(`(?i)\b(search the web|look up|google|find online)\b`),
	regexp.MustCompile(`(?i)\b(generate (an? )?image|draw|create (an? )?image)\b`),
	regexp.MustCompile(`(?i)\b(send (an? )?email|email .+ about)\b`),
	regexp.MustCompile(`(?i)\b(run (a )?command|execute|bash|shell)\b`),
}

func hasToolIntent(text string) bool {
	for _, p := range toolIntentPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

// Handler handles chat requests.
type Handler struct {
	db         *db.DB
	llm        *llm.Client
	authMgr    *auth.Manager
	agentFn    AgentFunc
	researchFn ResearchFunc
}

// AgentFunc is the signature for the agent loop streaming function.
type AgentFunc func(ctx context.Context, req AgentRequest, w http.ResponseWriter) error

// ResearchFunc is the signature for the research streaming function.
type ResearchFunc func(ctx context.Context, question string, endpointURL string, model string, headers map[string]string, sendEvent func(string, string)) error

// AgentRequest carries parameters to the agent loop.
type AgentRequest struct {
	SessionID   string
	Messages    []llm.Message
	Owner       string
	EndpointURL string
	Model       string
	Headers     map[string]string
}

// New creates a chat Handler.
func New(database *db.DB, llmClient *llm.Client, authMgr *auth.Manager, agentFn AgentFunc, researchFn ResearchFunc) *Handler {
	return &Handler{db: database, llm: llmClient, authMgr: authMgr, agentFn: agentFn, researchFn: researchFn}
}

// ServeHTTP handles POST /api/chat — streams the LLM response as SSE.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := auth.CurrentUser(r)

	var req struct {
		SessionID   string `json:"session_id"`
		Message     string `json:"message"`
		Mode        string `json:"mode"` // "chat" or "agent"
		EndpointURL string `json:"endpoint_url"`
		Model       string `json:"model"`
		UseResearch bool
	}
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "multipart/form-data") || strings.Contains(ct, "application/x-www-form-urlencoded") {
		r.ParseMultipartForm(1 << 20)
		req.SessionID = r.FormValue("session")
		if req.SessionID == "" {
			req.SessionID = r.FormValue("session_id")
		}
		req.Message = r.FormValue("message")
		req.Mode = r.FormValue("mode")
		req.EndpointURL = r.FormValue("endpoint_url")
		req.Model = r.FormValue("model")
		req.UseResearch = r.FormValue("use_research") == "true"
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// Load session
	sess, err := h.db.GetSession(req.SessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	// Ownership check
	if sess.Owner.Valid && sess.Owner.String != user && !h.authMgr.IsAdmin(user) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	// Persist user message
	userMsg := &db.ChatMessage{
		ID:        uuid.New().String(),
		SessionID: req.SessionID,
		Role:      "user",
		Content:   req.Message,
		Timestamp: time.Now().UTC(),
	}
	h.db.AddMessage(userMsg)

	// Build message history for LLM
	dbMsgs, _ := h.db.ListMessages(req.SessionID)
	messages := make([]llm.Message, 0, len(dbMsgs))
	for _, m := range dbMsgs {
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}

	endpointURL := req.EndpointURL
	if endpointURL == "" {
		endpointURL = sess.EndpointURL
	}
	model := req.Model
	if model == "" {
		model = sess.Model
	}

	// Look up API key from the matching endpoint
	headers := map[string]string{}
	if endpoints, err := h.db.ListModelEndpoints(user, h.authMgr.IsAdmin(user)); err == nil {
		normalizedURL := llm.NormalizeBaseURL(endpointURL)
		for _, ep := range endpoints {
			if llm.NormalizeBaseURL(ep.BaseURL) == normalizedURL {
				if ep.APIKey.Valid && ep.APIKey.String != "" && ep.APIKey.String != "stored" {
					headers["Authorization"] = "Bearer " + ep.APIKey.String
				}
				break
			}
		}
	}

	// Determine mode
	mode := req.Mode
	if mode == "" && sess.Mode.Valid {
		mode = sess.Mode.String
	}
	if mode != "agent" && hasToolIntent(req.Message) {
		mode = "agent"
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, canFlush := w.(http.Flusher)

	sendEvent := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if canFlush {
			flusher.Flush()
		}
	}

	ctx := r.Context()

	if req.UseResearch && h.researchFn != nil {
		if err := h.researchFn(ctx, req.Message, endpointURL, model, headers, sendEvent); err != nil {
			sendEvent("error", fmt.Sprintf(`{"error":%q,"text":%q}`, err.Error(), err.Error()))
		}
		sendEvent("done", `{"status":"done"}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok { f.Flush() }
		return
	}

	if mode == "agent" && h.agentFn != nil {
		if err := h.agentFn(ctx, AgentRequest{
			SessionID:   req.SessionID,
			Messages:    messages,
			Owner:       user,
			EndpointURL: endpointURL,
			Model:       model,
			Headers:     headers,
		}, w); err != nil {
			sendEvent("error", fmt.Sprintf(`{"error":%q,"text":%q}`, err.Error(), err.Error()))
		}
		return
	}

	// Plain chat streaming
	streamReq := llm.StreamRequest{
		URL:      endpointURL,
		Model:    model,
		Messages: messages,
		Headers:  headers,
	}

	ch, err := h.llm.Stream(ctx, streamReq)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":%q,"text":%q}`, err.Error(), err.Error()))
		return
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			sendEvent("error", fmt.Sprintf(`{"error":%q,"text":%q}`, chunk.Error.Error(), chunk.Error.Error()))
			break
		}
		if chunk.Delta != "" {
			sb.WriteString(chunk.Delta)
			data, _ := json.Marshal(map[string]string{"delta": chunk.Delta})
			sendEvent("delta", string(data))
		}
	}

	// Persist assistant response
	if sb.Len() > 0 {
		assistantMsg := &db.ChatMessage{
			ID:        uuid.New().String(),
			SessionID: req.SessionID,
			Role:      "assistant",
			Content:   sb.String(),
			Timestamp: time.Now().UTC(),
			Metadata:  sql.NullString{Valid: false},
		}
		h.db.AddMessage(assistantMsg)
	}

	sendEvent("done", `{"status":"done"}`)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
