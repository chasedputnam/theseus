package server

import (
	"context"
	"log"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/google/uuid"
)

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	mgr := s.memMgr
	switch r.Method {
	case http.MethodGet:
		entries, err := mgr.List(context.Background(), user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if entries == nil {
			entries = []*db.Memory{}
		}
		writeJSON(w, http.StatusOK, entries)
	case http.MethodPost:
		var req struct {
			Text     string `json:"text"`
			Category string `json:"category"`
			Source   string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Category == "" {
			req.Category = "fact"
		}
		if req.Source == "" {
			req.Source = "user"
		}
		entry, err := mgr.Add(context.Background(), req.Text, req.Category, req.Source, user, "")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, entry)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMemoryOps(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	mgr := s.memMgr
	path := strings.TrimPrefix(r.URL.Path, "/api/memory/")

	if path == "search" {
		q := r.URL.Query().Get("q")
		results, err := mgr.Search(context.Background(), q, user, 10)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if results == nil {
			results = []*db.Memory{}
		}
		writeJSON(w, http.StatusOK, results)
		return
	}

	if path == "import" && r.Method == http.MethodPost {
		var entries []*db.Memory
		if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		added, err := mgr.Import(context.Background(), entries, user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"added": added})
		return
	}

	if r.Method == http.MethodDelete {
		if err := mgr.Delete(context.Background(), path, user); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)

	var req struct {
		SessionID   string `json:"session_id"`
		Message     string `json:"message"`
		Mode        string `json:"mode"`
		EndpointURL string `json:"endpoint_url"`
		Model       string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	sess, err := s.db.GetSession(req.SessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	if sess.Owner.Valid && sess.Owner.String != user && !s.auth.IsAdmin(user) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	userMsg := &db.ChatMessage{
		ID:        uuid.New().String(),
		SessionID: req.SessionID,
		Role:      "user",
		Content:   req.Message,
		Timestamp: time.Now().UTC(),
	}
	if err := s.db.AddMessage(userMsg); err != nil {
		log.Printf("chat: persist user message: %v", err)
	}

	dbMsgs, err := s.db.ListMessages(req.SessionID)
	if err != nil {
		log.Printf("chat: list messages: %v", err)
		dbMsgs = nil
	}
	endpointURL := req.EndpointURL
	if endpointURL == "" {
		endpointURL = sess.EndpointURL
	}
	model := req.Model
	if model == "" {
		model = sess.Model
	}

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

	messages := make([]llm.Message, 0, len(dbMsgs))
	for _, m := range dbMsgs {
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}

	llmClient := llm.New()
	ch, err := llmClient.Stream(r.Context(), llm.StreamRequest{
		URL:      endpointURL,
		Model:    model,
		Messages: messages,
	})
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
			d, _ := json.Marshal(map[string]string{"content": chunk.Delta})
			sendEvent("delta", string(d))
		}
	}

	if sb.Len() > 0 {
		assistantMsg := &db.ChatMessage{
			ID:        uuid.New().String(),
			SessionID: req.SessionID,
			Role:      "assistant",
			Content:   sb.String(),
			Timestamp: time.Now().UTC(),
			Metadata:  sql.NullString{Valid: false},
		}
		if err := s.db.AddMessage(assistantMsg); err != nil {
			log.Printf("chat: persist assistant message: %v", err)
		}
	}
	sendEvent("done", `{"status":"done"}`)
}
