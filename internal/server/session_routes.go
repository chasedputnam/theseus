package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

func (s *Server) registerSessionRoutes() {
	s.mux.HandleFunc("/api/sessions", s.withAuth(s.handleSessions))
	s.mux.HandleFunc("/api/sessions/", s.withAuth(s.handleSessionByID))
	s.mux.HandleFunc("/api/settings", s.withAuth(s.handleSettings))
	s.mux.HandleFunc("/api/features", s.withAuth(s.handleFeatures))
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		archived := r.URL.Query().Get("archived") == "true"
		sessions, err := s.db.ListSessions(user, archived)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if sessions == nil {
			sessions = []*db.Session{}
		}
		epURLToID := s.buildEndpointURLMap(user)
		type sessionResponse struct {
			*db.Session
			HasDocuments bool   `json:"has_documents"`
			HasImages    bool   `json:"has_images"`
			IsOpenClaw   bool   `json:"is_openclaw"`
			EndpointID   string `json:"endpoint_id"`
		}
		result := make([]sessionResponse, 0, len(sessions))
		for _, sess := range sessions {
			hasDocs, _ := s.db.SessionHasDocuments(sess.ID)
			hasImgs, _ := s.db.SessionHasImages(sess.ID)
			isOC := sess.Mode.Valid && sess.Mode.String == "openclaw"
			result = append(result, sessionResponse{
				Session:      sess,
				HasDocuments: hasDocs,
				HasImages:    hasImgs,
				IsOpenClaw:   isOC,
				EndpointID:   epURLToID[sess.EndpointURL],
			})
		}
		writeJSON(w, http.StatusOK, result)
	case http.MethodPost:
		var name, endpointURL, model, folder, endpointID string
		var rag bool
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "multipart/form-data") || strings.Contains(ct, "application/x-www-form-urlencoded") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			name = r.FormValue("name")
			if name == "" {
				name = r.FormValue("title")
			}
			endpointURL = r.FormValue("endpoint_url")
			model = r.FormValue("model")
			folder = r.FormValue("folder")
			endpointID = r.FormValue("endpoint_id")
			rag = r.FormValue("rag") == "true"
		} else {
			var req struct {
				Name        string `json:"name"`
				Title       string `json:"title"`
				EndpointURL string `json:"endpoint_url"`
				Model       string `json:"model"`
				Folder      string `json:"folder"`
				EndpointID  string `json:"endpoint_id"`
				RAG         bool   `json:"rag"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			name = req.Name
			if name == "" {
				name = req.Title
			}
			endpointURL = req.EndpointURL
			model = req.Model
			folder = req.Folder
			endpointID = req.EndpointID
			rag = req.RAG
		}
		// Resolve endpoint URL from endpoint_id if not provided directly
		if endpointURL == "" && endpointID != "" {
			if ep, err := s.db.GetModelEndpoint(endpointID); err == nil {
				endpointURL = ep.BaseURL
				if model == "" && ep.CachedModels.Valid {
					var models []string
					if json.Unmarshal([]byte(ep.CachedModels.String), &models) == nil && len(models) > 0 {
						model = models[0]
					}
				}
			}
		}
		sess := &db.Session{
			ID:          uuid.New().String(),
			Name:        name,
			EndpointURL: endpointURL,
			Model:       model,
			RAG:         rag,
			Owner:       sql.NullString{String: user, Valid: user != ""},
			Headers:     "{}",
		}
		if folder != "" {
			sess.Folder = sql.NullString{String: folder, Valid: true}
		}
		if err := s.db.CreateSession(sess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sess)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// buildEndpointURLMap returns a map of base_url -> endpoint ID for the given user.
func (s *Server) buildEndpointURLMap(user string) map[string]string {
	eps, err := s.db.ListModelEndpoints(user, false)
	if err != nil {
		return map[string]string{}
	}
	m := make(map[string]string, len(eps))
	for _, ep := range eps {
		m[ep.BaseURL] = ep.ID
	}
	return m
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	sess, err := s.db.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if sess.Owner.Valid && sess.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	switch action {
	case "messages":
		msgs, err := s.db.ListMessages(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if msgs == nil {
			msgs = []*db.ChatMessage{}
		}
		writeJSON(w, http.StatusOK, msgs)
		return
	case "compact":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		msgs, _ := s.db.ListMessages(id)
		if len(msgs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": ""})
			return
		}
		var sb strings.Builder
		for _, m := range msgs {
			fmt.Fprintf(&sb, "%s: %s\n", m.Role, m.Content)
		}
		endpointURL := sess.EndpointURL
		if endpointURL == "" {
			endpointURL = settings.GetString("default_endpoint_url")
		}
		if endpointURL == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no endpoint configured for this session"})
			return
		}
		client := llm.New()
		headers := map[string]string{}
		if endpoints, err := s.db.ListModelEndpoints(user, s.auth.IsAdmin(user)); err == nil {
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
		ch, err := client.Stream(r.Context(), llm.StreamRequest{
			URL:     endpointURL,
			Model:   sess.Model,
			Headers: headers,
			Messages: []llm.Message{
				{Role: "user", Content: "Summarize this conversation concisely:\n" + sb.String()},
			},
		})
		var summary strings.Builder
		if err == nil {
			for chunk := range ch {
				if chunk.Error == nil {
					summary.WriteString(chunk.Delta)
				}
			}
		}
		compactFile := s.cfg.DataDir + "/compact_" + sanitizeFilename(id) + ".json"
		storage.WriteJSON(compactFile, map[string]string{"summary": summary.String()})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": summary.String()})
		return
	case "context_info":
		msgs, _ := s.db.ListMessages(id)
		tokenEst := 0
		for _, m := range msgs {
			tokenEst += len(m.Content) / 4
		}
		compactFile := s.cfg.DataDir + "/compact_" + sanitizeFilename(id) + ".json"
		var compact map[string]string
		hasCompact := storage.ReadJSON(compactFile, &compact) == nil && compact["summary"] != ""
		writeJSON(w, http.StatusOK, map[string]any{
			"message_count":  len(msgs),
			"token_estimate": tokenEst,
			"has_compact":    hasCompact,
		})
		return
	case "archive":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sess.Archived = true
		if err := s.db.UpdateSession(sess); err != nil {
			log.Printf("db: UpdateSession: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "unarchive":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sess.Archived = false
		if err := s.db.UpdateSession(sess); err != nil {
			log.Printf("db: UpdateSession: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "star":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sess.IsImportant = !sess.IsImportant
		if err := s.db.UpdateSession(sess); err != nil {
			log.Printf("db: UpdateSession: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"is_important": sess.IsImportant})
		return
	case "important":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			r.ParseForm()
		}
		if v := r.FormValue("important"); v != "" {
			sess.IsImportant = v == "true" || v == "1"
		} else {
			sess.IsImportant = !sess.IsImportant
		}
		if err := s.db.UpdateSession(sess); err != nil {
			log.Printf("db: UpdateSession: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"is_important": sess.IsImportant})
		return
	case "restore":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sess.Archived = false
		if err := s.db.UpdateSession(sess); err != nil {
			log.Printf("db: UpdateSession: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "mark-stopped":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "fork":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		newSess := &db.Session{
			ID:          uuid.New().String(),
			Name:        sess.Name + " (fork)",
			EndpointURL: sess.EndpointURL,
			Model:       sess.Model,
			RAG:         sess.RAG,
			Owner:       sess.Owner,
			Headers:     sess.Headers,
			Mode:        sess.Mode,
		}
		if err := s.db.CreateSession(newSess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var forkReq struct {
			KeepCount int `json:"keep_count"`
		}
		json.NewDecoder(r.Body).Decode(&forkReq)
		msgs, _ := s.db.ListMessages(id)
		if forkReq.KeepCount > 0 && forkReq.KeepCount < len(msgs) {
			msgs = msgs[:forkReq.KeepCount]
		}
		for _, m := range msgs {
			newMsg := &db.ChatMessage{
				ID:        uuid.New().String(),
				SessionID: newSess.ID,
				Role:      m.Role,
				Content:   m.Content,
				Metadata:  m.Metadata,
				Timestamp: m.Timestamp,
			}
			s.db.AddMessage(newMsg)
		}
		writeJSON(w, http.StatusOK, newSess)
		return
	case "truncate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			AfterMessageID string `json:"after_message_id"`
			KeepCount      int    `json:"keep_count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.AfterMessageID == "" && req.KeepCount > 0 {
			msgs, _ := s.db.ListMessages(id)
			if req.KeepCount < len(msgs) {
				req.AfterMessageID = msgs[req.KeepCount-1].ID
			}
		}
		if req.AfterMessageID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "after_message_id required"})
			return
		}
		if err := s.db.TruncateMessages(id, req.AfterMessageID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "delete-messages":
		if r.Method != http.MethodDelete && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			MsgIDs []string `json:"msg_ids"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.MsgIDs) > 0 {
			for _, msgID := range req.MsgIDs {
				s.db.DeleteMessageByID(msgID)
			}
		} else {
			if err := s.db.DeleteMessages(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "edit-message":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			MessageID string `json:"message_id"`
			MsgID     string `json:"msg_id"`
			Content   string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message_id required"})
			return
		}
		if req.MessageID == "" {
			req.MessageID = req.MsgID
		}
		if req.MessageID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message_id required"})
			return
		}
		if err := s.db.UpdateMessage(req.MessageID, req.Content, sql.NullString{}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "merge-last-assistant":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		msgs, _ := s.db.ListMessages(id)
		var assistantMsgs []*db.ChatMessage
		for _, m := range msgs {
			if m.Role == "assistant" {
				assistantMsgs = append(assistantMsgs, m)
			}
		}
		if len(assistantMsgs) < 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need at least 2 assistant messages"})
			return
		}
		last := assistantMsgs[len(assistantMsgs)-1]
		prev := assistantMsgs[len(assistantMsgs)-2]
		merged := prev.Content + "\n" + last.Content
		s.db.UpdateMessage(prev.ID, merged, prev.Metadata)
		s.db.DeleteMessageByID(last.ID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "update-last-meta":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var meta map[string]any
		if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		msgs, _ := s.db.ListMessages(id)
		if len(msgs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no messages"})
			return
		}
		last := msgs[len(msgs)-1]
		metaJSON, _ := json.Marshal(meta)
		s.db.UpdateMessage(last.ID, last.Content, sql.NullString{String: string(metaJSON), Valid: true})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "inject_messages":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		for _, m := range req.Messages {
			msg := &db.ChatMessage{
				ID:        uuid.New().String(),
				SessionID: id,
				Role:      m.Role,
				Content:   m.Content,
				Timestamp: time.Now().UTC(),
			}
			s.db.AddMessage(msg)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(req.Messages)})
		return
	case "message":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role and content required"})
			return
		}
		msg := &db.ChatMessage{
			ID:        uuid.New().String(),
			SessionID: id,
			Role:      req.Role,
			Content:   req.Content,
			Timestamp: time.Now().UTC(),
		}
		if err := s.db.AddMessage(msg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, msg)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, sess)
	case http.MethodPut, http.MethodPatch:
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			var req struct {
				Name        *string `json:"name"`
				EndpointURL *string `json:"endpoint_url"`
				Model       *string `json:"model"`
				RAG         *bool   `json:"rag"`
				Folder      *string `json:"folder"`
				IsImportant *bool   `json:"is_important"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			if req.Name != nil {
				sess.Name = *req.Name
			}
			if req.EndpointURL != nil {
				sess.EndpointURL = *req.EndpointURL
			}
			if req.Model != nil {
				sess.Model = *req.Model
			}
			if req.RAG != nil {
				sess.RAG = *req.RAG
			}
			if req.Folder != nil {
				sess.Folder = sql.NullString{String: *req.Folder, Valid: *req.Folder != ""}
			}
			if req.IsImportant != nil {
				sess.IsImportant = *req.IsImportant
			}
		} else {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			if v := r.FormValue("name"); v != "" {
				sess.Name = v
			}
			if v := r.FormValue("endpoint_url"); v != "" {
				sess.EndpointURL = v
			}
			if v := r.FormValue("model"); v != "" {
				sess.Model = v
			}
			if v := r.FormValue("folder"); v != "" || r.Form.Has("folder") {
				sess.Folder = sql.NullString{String: v, Valid: v != ""}
			}
			if v := r.FormValue("endpoint_id"); v != "" {
				if ep, err := s.db.GetModelEndpoint(v); err == nil {
					if sess.EndpointURL == "" {
						sess.EndpointURL = ep.BaseURL
					}
				}
			}
			if v := r.FormValue("rag"); v != "" {
				sess.RAG = v == "true" || v == "1"
			}
			if v := r.FormValue("is_important"); v != "" {
				sess.IsImportant = v == "true" || v == "1"
			}
		}
		if err := s.db.UpdateSession(sess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sess)
	case http.MethodDelete:
		if err := s.db.DeleteSession(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, settings.Load())
	case http.MethodPost:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		current := settings.Load()
		for k, v := range req {
			current[k] = v
		}
		if err := settings.Save(current); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, settings.LoadFeatures())
	case http.MethodPost:
		var req map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		current := settings.LoadFeatures()
		for k, v := range req {
			current[k] = v
		}
		if err := settings.SaveFeatures(current); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
