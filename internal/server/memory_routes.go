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
	"github.com/chaseputnam/theseus/internal/settings"
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

	if path == "add" && r.Method == http.MethodPost {
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
		return
	}

	if path == "search" {
		q := r.URL.Query().Get("q")
		if q == "" && r.Method == http.MethodPost {
			ct := r.Header.Get("Content-Type")
			if strings.Contains(ct, "multipart/form-data") || strings.Contains(ct, "application/x-www-form-urlencoded") {
				r.ParseMultipartForm(1 << 20)
				q = r.FormValue("query")
				if q == "" {
					q = r.FormValue("q")
				}
			} else {
				var body struct {
					Query string `json:"query"`
					Q     string `json:"q"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				q = body.Query
				if q == "" {
					q = body.Q
				}
			}
		}
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
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form"})
				return
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
				return
			}
			defer f.Close()
			if err := json.NewDecoder(f).Decode(&entries); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON in file"})
				return
			}
		} else {
			if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
		}
		added, err := mgr.Import(context.Background(), entries, user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"added": added})
		return
	}

	if path == "audit" && r.Method == http.MethodPost {
		all, err := mgr.List(context.Background(), user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		seen := map[string]bool{}
		removed := 0
		for _, m := range all {
			if seen[m.Text] {
				mgr.Delete(context.Background(), m.ID, user)
				removed++
			} else {
				seen[m.Text] = true
			}
		}
		writeJSON(w, http.StatusOK, map[string]int{"removed": removed, "merged": 0})
		return
	}

	if path == "extract" && r.Method == http.MethodPost {
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
			return
		}
		endpointURL := settings.GetString("default_endpoint_url")
		model := settings.GetString("default_model")
		prompt := "Extract distinct facts from the following text. Return a JSON array of strings, one fact per element.\n\n" + req.Text
		var result strings.Builder
		lc := llm.New()
		ch, err := lc.Stream(r.Context(), llm.StreamRequest{
			URL:   endpointURL,
			Model: model,
			Messages: []llm.Message{{Role: "user", Content: prompt}},
		})
		if err == nil {
			for chunk := range ch {
				if chunk.Error == nil {
					result.WriteString(chunk.Delta)
				}
			}
		}
		var facts []string
		if err := json.Unmarshal([]byte(result.String()), &facts); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"stored": 0, "raw": result.String()})
			return
		}
		stored := 0
		for _, fact := range facts {
			if fact != "" {
				mgr.Add(context.Background(), fact, "fact", "extract", user, "")
				stored++
			}
		}
		writeJSON(w, http.StatusOK, map[string]int{"stored": stored})
		return
	}

	// pin/{id}
	if strings.HasPrefix(path, "pin/") && r.Method == http.MethodPost {
		id := strings.TrimPrefix(path, "pin/")
		mem, err := s.db.GetMemory(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		newPinned := !mem.Pinned
		if err := s.db.PinMemory(id, user, newPinned); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		mem.Pinned = newPinned
		writeJSON(w, http.StatusOK, mem)
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
		SessionID   string
		Message     string
		Mode        string
		EndpointURL string
		Model       string
	}

	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var body struct {
			SessionID   string `json:"session_id"`
			Message     string `json:"message"`
			Mode        string `json:"mode"`
			EndpointURL string `json:"endpoint_url"`
			Model       string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		req.SessionID = body.SessionID
		req.Message = body.Message
		req.Mode = body.Mode
		req.EndpointURL = body.EndpointURL
		req.Model = body.Model
	} else {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			r.ParseForm()
		}
		req.SessionID = r.FormValue("session")
		if req.SessionID == "" {
			req.SessionID = r.FormValue("session_id")
		}
		req.Message = r.FormValue("message")
		req.Mode = r.FormValue("mode")
		req.EndpointURL = r.FormValue("endpoint_url")
		req.Model = r.FormValue("model")
	}

	if req.SessionID == "" || req.Message == "" {
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
