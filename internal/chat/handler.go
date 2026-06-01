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
	agentFn    AgentFunc // injected to avoid circular import
}

// AgentFunc is the signature for the agent loop streaming function.
type AgentFunc func(ctx context.Context, req AgentRequest, w http.ResponseWriter) error

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
func New(database *db.DB, llmClient *llm.Client, authMgr *auth.Manager, agentFn AgentFunc) *Handler {
	return &Handler{db: database, llm: llmClient, authMgr: authMgr, agentFn: agentFn}
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	if mode == "agent" && h.agentFn != nil {
		if err := h.agentFn(ctx, AgentRequest{
			SessionID:   req.SessionID,
			Messages:    messages,
			Owner:       user,
			EndpointURL: endpointURL,
			Model:       model,
		}, w); err != nil {
			sendEvent("error", fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		return
	}

	// Plain chat streaming
	streamReq := llm.StreamRequest{
		URL:      endpointURL,
		Model:    model,
		Messages: messages,
	}

	ch, err := h.llm.Stream(ctx, streamReq)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			sendEvent("error", fmt.Sprintf(`{"error":%q}`, chunk.Error.Error()))
			break
		}
		if chunk.Delta != "" {
			sb.WriteString(chunk.Delta)
			data, _ := json.Marshal(map[string]string{"content": chunk.Delta})
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
}
