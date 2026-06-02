package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/email"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

// ScheduledEmail represents a queued outbound email.
type ScheduledEmail struct {
	ID        string    `json:"id"`
	SendAt    time.Time `json:"send_at"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	AccountID string    `json:"account_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) emailDraftFile(user string) string {
	return s.cfg.DataDir + "/email_drafts_" + sanitizeFilename(user) + ".json"
}

func (s *Server) emailScheduledFile(user string) string {
	return s.cfg.DataDir + "/email_scheduled_" + sanitizeFilename(user) + ".json"
}

func (s *Server) handleEmailDraft(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	file := s.emailDraftFile(user)
	switch r.Method {
	case http.MethodGet:
		var draft map[string]any
		if err := storage.ReadJSON(file, &draft); err != nil || draft == nil {
			draft = map[string]any{}
		}
		writeJSON(w, http.StatusOK, draft)
	case http.MethodPost, http.MethodPut:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		storage.WriteJSON(file, req)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		storage.WriteJSON(file, map[string]any{})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEmailSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	var req ScheduledEmail
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.ID = uuid.New().String()
	req.CreatedAt = time.Now().UTC()

	var scheduled []ScheduledEmail
	storage.ReadJSON(s.emailScheduledFile(user), &scheduled)
	scheduled = append(scheduled, req)
	storage.WriteJSON(s.emailScheduledFile(user), scheduled)
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleEmailScheduled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	var scheduled []ScheduledEmail
	storage.ReadJSON(s.emailScheduledFile(user), &scheduled)
	if scheduled == nil {
		scheduled = []ScheduledEmail{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scheduled": scheduled})
}

func (s *Server) handleEmailScheduledByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/email/scheduled/"), "/")

	var scheduled []ScheduledEmail
	storage.ReadJSON(s.emailScheduledFile(user), &scheduled)
	updated := scheduled[:0]
	found := false
	for _, se := range scheduled {
		if se.ID == id {
			found = true
			continue
		}
		updated = append(updated, se)
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	storage.WriteJSON(s.emailScheduledFile(user), updated)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailSummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	var req struct {
		UID    uint32 `json:"uid"`
		Folder string `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Folder == "" {
		req.Folder = "INBOX"
	}
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "email not configured"})
		return
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	msg, err := client.FetchMessage(context.Background(), req.Folder, req.UID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	body := msg.Body
	if body == "" {
		body = msg.HTMLBody
	}
	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	var summary strings.Builder
	lc := llm.New()
	ch, err := lc.Stream(r.Context(), llm.StreamRequest{
		URL:   endpointURL,
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: "Summarize the following email in 2-3 sentences."},
			{Role: "user", Content: body},
		},
	})
	if err == nil {
		for chunk := range ch {
			if chunk.Error == nil {
				summary.WriteString(chunk.Delta)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"summary": summary.String()})
}

func (s *Server) handleEmailAIReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	var req struct {
		UID    uint32 `json:"uid"`
		Folder string `json:"folder"`
		Tone   string `json:"tone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Folder == "" {
		req.Folder = "INBOX"
	}
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "email not configured"})
		return
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	msg, err := client.FetchMessage(context.Background(), req.Folder, req.UID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	body := msg.Body
	if body == "" {
		body = msg.HTMLBody
	}
	tone := req.Tone
	if tone == "" {
		tone = "professional"
	}
	prompt := "Draft a " + tone + " reply to the following email. Return only the reply body.\n\n" + body
	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	var reply strings.Builder
	lc := llm.New()
	ch, err := lc.Stream(r.Context(), llm.StreamRequest{
		URL:   endpointURL,
		Model: model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err == nil {
		for chunk := range ch {
			if chunk.Error == nil {
				reply.WriteString(chunk.Delta)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"reply": reply.String()})
}

func (s *Server) handleEmailUrgencyState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusOK, map[string]any{"folders": map[string]int{}})
		return
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	folders, err := client.ListFolders(context.Background())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	counts := map[string]int{}
	for _, folder := range folders {
		msgs, err := client.ListMessages(context.Background(), folder, 1000, true)
		if err == nil {
			counts[folder] = len(msgs)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": counts})
}

func (s *Server) handleEmailAttachmentAsDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	seg := strings.TrimPrefix(r.URL.Path, "/api/email/attachment-as-doc/")
	parts := strings.SplitN(seg, "/", 2)
	if len(parts) != 2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	uid, err := uidFromPath("/api/email/attachment-as-doc/"+parts[0], "/api/email/attachment-as-doc/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid uid"})
		return
	}
	idxStr := strings.TrimSuffix(parts[1], "/")
	idx := 0
	if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid index"})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "email not configured"})
		return
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	att, err := client.FetchAttachment(context.Background(), folder, uid, idx)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Only store text-based attachments as document content.
	ct := att.ContentType
	var content string
	switch {
	case strings.HasPrefix(ct, "text/"):
		content = string(att.Data)
	case ct == "application/json":
		content = string(att.Data)
	default:
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
			"error": "attachment type " + ct + " cannot be stored as a document; only text attachments are supported",
		})
		return
	}
	doc := &db.Document{
		ID:             uuid.New().String(),
		Title:          att.Filename,
		CurrentContent: content,
		IsActive:       true,
		Owner:          sql.NullString{String: user, Valid: true},
	}
	if err := s.db.CreateDocument(doc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "document_id": doc.ID})
}

func (s *Server) handleEmailExtractStyle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	var req struct {
		UIDs   []uint32 `json:"uids"`
		Folder string   `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Folder == "" {
		req.Folder = "INBOX"
	}
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "email not configured"})
		return
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()

	var samples strings.Builder
	for i, uid := range req.UIDs {
		if i >= 5 {
			break
		}
		msg, err := client.FetchMessage(context.Background(), req.Folder, uid)
		if err != nil {
			continue
		}
		body := msg.Body
		if body == "" {
			body = msg.HTMLBody
		}
		samples.WriteString("---\n")
		samples.WriteString(body)
		samples.WriteString("\n")
	}

	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	prompt := "Analyze the writing style of the following emails and describe it in a concise paragraph covering tone, vocabulary, sentence structure, and formality level.\n\n" + samples.String()
	var styleOut strings.Builder
	lc := llm.New()
	ch, err := lc.Stream(r.Context(), llm.StreamRequest{
		URL:   endpointURL,
		Model: model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err == nil {
		for chunk := range ch {
			if chunk.Error == nil {
				styleOut.WriteString(chunk.Delta)
			}
		}
	}

	styleResult := styleOut.String()
	file := s.emailStyleFile(user)
	var existing map[string]any
	storage.ReadJSON(file, &existing)
	if existing == nil {
		existing = map[string]any{}
	}
	existing["extracted_style"] = styleResult
	storage.WriteJSON(file, existing)

	writeJSON(w, http.StatusOK, map[string]string{"style": styleResult})
}
