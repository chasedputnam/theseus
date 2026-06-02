package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/email"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
	imap "github.com/emersion/go-imap/v2"
)

// EmailAccount holds per-account IMAP/SMTP configuration.
type EmailAccount struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	IMAPHost     string    `json:"imap_host"`
	IMAPPort     int       `json:"imap_port"`
	IMAPUser     string    `json:"imap_user"`
	IMAPPassword string    `json:"imap_password"` // AES-256-GCM encrypted at rest
	IMAPStartTLS bool      `json:"imap_starttls"`
	SMTPHost     string    `json:"smtp_host"`
	SMTPPort     int       `json:"smtp_port"`
	SMTPUser     string    `json:"smtp_user"`
	SMTPPassword string    `json:"smtp_password"` // AES-256-GCM encrypted at rest
	FromAddress  string    `json:"from_address"`
	IsDefault    bool      `json:"is_default"`
	Owner        string    `json:"owner"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Server) emailAccountsFile(user string) string {
	return s.cfg.DataDir + "/email_accounts_" + sanitizeFilename(user) + ".json"
}

func (s *Server) loadEmailAccounts(user string) []*EmailAccount {
	var accounts []*EmailAccount
	storage.ReadJSON(s.emailAccountsFile(user), &accounts)
	if accounts == nil {
		accounts = []*EmailAccount{}
	}
	return accounts
}

func (s *Server) saveEmailAccounts(user string, accounts []*EmailAccount) error {
	return storage.WriteJSON(s.emailAccountsFile(user), accounts)
}

func maskEmailAccount(a *EmailAccount) map[string]any {
	return map[string]any{
		"id": a.ID, "name": a.Name,
		"imap_host": a.IMAPHost, "imap_port": a.IMAPPort,
		"imap_user": a.IMAPUser, "imap_password": "",
		"imap_starttls": a.IMAPStartTLS,
		"smtp_host": a.SMTPHost, "smtp_port": a.SMTPPort,
		"smtp_user": a.SMTPUser, "smtp_password": "",
		"from_address": a.FromAddress, "owner": a.Owner,
		"created_at": a.CreatedAt,
	}
}

func (s *Server) emailStyleFile(user string) string {
	return s.cfg.DataDir + "/email_style_" + sanitizeFilename(user) + ".json"
}

func (s *Server) registerEmailRoutes() {
	s.mux.HandleFunc("/api/email/config", s.withAuth(s.handleEmailConfig))
	s.mux.HandleFunc("/api/email/accounts", s.withAuth(s.handleEmailAccounts))
	s.mux.HandleFunc("/api/email/accounts/test", s.withAuth(s.handleEmailAccountTest))
	s.mux.HandleFunc("/api/email/accounts/", s.withAuth(s.handleEmailAccountByID))
	s.mux.HandleFunc("/api/email/style", s.withAuth(s.handleEmailStyle))
	s.mux.HandleFunc("/api/email/folders", s.withAuth(s.handleEmailFolders))
	s.mux.HandleFunc("/api/email/messages", s.withAuth(s.handleEmailMessages))
	s.mux.HandleFunc("/api/email/send", s.withAuth(s.handleEmailSend))
	s.mux.HandleFunc("/api/email/mark-read", s.withAuth(s.handleEmailMarkRead))
	s.mux.HandleFunc("/api/email/delete", s.withAuth(s.handleEmailDelete))
	s.mux.HandleFunc("/api/email/move", s.withAuth(s.handleEmailMove))
	// Per-message routes (frontend uses UID in path)
	s.mux.HandleFunc("/api/email/list", s.withAuth(s.handleEmailList))
	s.mux.HandleFunc("/api/email/read/", s.withAuth(s.handleEmailReadByUID))
	s.mux.HandleFunc("/api/email/mark-read/", s.withAuth(s.handleEmailMarkReadByUID))
	s.mux.HandleFunc("/api/email/mark-unread/", s.withAuth(s.handleEmailMarkUnreadByUID))
	s.mux.HandleFunc("/api/email/mark-answered/", s.withAuth(s.handleEmailMarkAnsweredByUID))
	s.mux.HandleFunc("/api/email/clear-answered/", s.withAuth(s.handleEmailClearAnsweredByUID))
	s.mux.HandleFunc("/api/email/archive/", s.withAuth(s.handleEmailArchiveByUID))
	s.mux.HandleFunc("/api/email/delete/", s.withAuth(s.handleEmailDeleteByUID))
	s.mux.HandleFunc("/api/email/delete-permanent/", s.withAuth(s.handleEmailDeletePermanentByUID))
	s.mux.HandleFunc("/api/email/move/", s.withAuth(s.handleEmailMoveByUID))
	s.mux.HandleFunc("/api/email/search", s.withAuth(s.handleEmailSearch))
	s.mux.HandleFunc("/api/email/draft", s.withAuth(s.handleEmailDraft))
	s.mux.HandleFunc("/api/email/schedule", s.withAuth(s.handleEmailSchedule))
	s.mux.HandleFunc("/api/email/scheduled", s.withAuth(s.handleEmailScheduled))
	s.mux.HandleFunc("/api/email/scheduled/", s.withAuth(s.handleEmailScheduledByID))
	s.mux.HandleFunc("/api/email/summarize", s.withAuth(s.handleEmailSummarize))
	s.mux.HandleFunc("/api/email/urgency-state", s.withAuth(s.handleEmailUrgencyState))
	s.mux.HandleFunc("/api/email/ai-reply", s.withAuth(s.handleEmailAIReply))
	s.mux.HandleFunc("/api/email/compose-upload", s.withAuth(s.handleEmailComposeUpload))
	s.mux.HandleFunc("/api/email/attachment/", s.withAuth(s.handleEmailAttachment))
	s.mux.HandleFunc("/api/email/attachment-as-doc/", s.withAuth(s.handleEmailAttachmentAsDoc))
	s.mux.HandleFunc("/api/email/odysseus/reminders", s.withAuth(s.handleEmailReminders))
	s.mux.HandleFunc("/api/email/unflag-spam/", s.withAuth(s.handleEmailUnflagSpam))
	s.mux.HandleFunc("/api/email/extract-style", s.withAuth(s.handleEmailExtractStyle))
}

func (s *Server) emailConfig() email.AccountConfig {
	return email.AccountConfig{
		IMAPHost:     settings.GetString("imap_host"),
		IMAPPort:     settings.GetInt("imap_port"),
		IMAPUser:     settings.GetString("imap_user"),
		IMAPPassword: settings.GetString("imap_password"),
		IMAPStartTLS: settings.GetBool("imap_starttls"),
		SMTPHost:     settings.GetString("smtp_host"),
		SMTPPort:     settings.GetInt("smtp_port"),
		SMTPUser:     settings.GetString("smtp_user"),
		SMTPPassword: settings.GetString("smtp_password"),
		FromAddress:  settings.GetString("smtp_from"),
	}
}

func (s *Server) handleEmailConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.emailConfig()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"imap_host":     cfg.IMAPHost,
			"imap_port":     cfg.IMAPPort,
			"imap_user":     cfg.IMAPUser,
			"imap_starttls": cfg.IMAPStartTLS,
			"smtp_host":     cfg.SMTPHost,
			"smtp_port":     cfg.SMTPPort,
			"smtp_user":     cfg.SMTPUser,
			"smtp_from":     cfg.FromAddress,
			"configured":    cfg.IMAPHost != "" && cfg.IMAPUser != "",
		})
	case http.MethodPost, http.MethodPut:
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
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEmailFolders(w http.ResponseWriter, r *http.Request) {
	cfg := s.emailConfig()
	if cfg.IMAPHost == "" {
		writeJSON(w, http.StatusOK, map[string]any{"folders": []string{}})
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
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (s *Server) handleEmailMessages(w http.ResponseWriter, r *http.Request) {
	cfg := s.emailConfig()
	if cfg.IMAPHost == "" {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"

	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	msgs, err := client.ListMessages(context.Background(), folder, limit, unreadOnly)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []*email.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "total": len(msgs)})
}

func (s *Server) handleEmailSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.emailConfig()
	var req email.SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := email.Send(cfg, req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.emailConfig()
	var req struct {
		Folder string   `json:"folder"`
		UIDs   []uint32 `json:"uids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Folder == "" {
		req.Folder = "INBOX"
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	uids := make([]imap.UID, len(req.UIDs))
	for i, u := range req.UIDs {
		uids[i] = imap.UID(u)
	}
	if err := client.MarkRead(context.Background(), req.Folder, uids); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.emailConfig()
	var req struct {
		Folder string   `json:"folder"`
		UIDs   []uint32 `json:"uids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Folder == "" {
		req.Folder = "INBOX"
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	uids := make([]imap.UID, len(req.UIDs))
	for i, u := range req.UIDs {
		uids[i] = imap.UID(u)
	}
	if err := client.DeleteMessages(context.Background(), req.Folder, uids); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.emailConfig()
	var req struct {
		Folder string   `json:"folder"`
		Dest   string   `json:"dest"`
		UIDs   []uint32 `json:"uids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	uids := make([]imap.UID, len(req.UIDs))
	for i, u := range req.UIDs {
		uids[i] = imap.UID(u)
	}
	if err := client.MoveMessages(context.Background(), req.Folder, req.Dest, uids); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailAccounts(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		accounts := s.loadEmailAccounts(user)
		masked := make([]map[string]any, len(accounts))
		for i, a := range accounts {
			masked[i] = maskEmailAccount(a)
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": masked})
	case http.MethodPost:
		var req EmailAccount
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		req.ID = uuid.New().String()
		req.Owner = user
		req.CreatedAt = time.Now().UTC()
		if req.IMAPPassword != "" {
			enc, err := storage.Encrypt(req.IMAPPassword)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encrypt failed"})
				return
			}
			req.IMAPPassword = enc
		}
		if req.SMTPPassword != "" {
			enc, err := storage.Encrypt(req.SMTPPassword)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encrypt failed"})
				return
			}
			req.SMTPPassword = enc
		}
		accounts := s.loadEmailAccounts(user)
		accounts = append(accounts, &req)
		s.saveEmailAccounts(user, accounts)
		writeJSON(w, http.StatusOK, maskEmailAccount(&req))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEmailAccountByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	path := strings.TrimPrefix(r.URL.Path, "/api/email/accounts/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	accounts := s.loadEmailAccounts(user)
	// Handle sub-actions
	if action == "set-default" && r.Method == http.MethodPost {
		for i, a := range accounts {
			accounts[i].IsDefault = a.ID == id
		}
		s.saveEmailAccounts(user, accounts)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var req EmailAccount
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		for i, a := range accounts {
			if a.ID == id {
				req.ID = id
				req.Owner = user
				req.CreatedAt = a.CreatedAt
				if req.IMAPPassword != "" && !storage.IsEncrypted(req.IMAPPassword) {
					enc, _ := storage.Encrypt(req.IMAPPassword)
					req.IMAPPassword = enc
				} else if req.IMAPPassword == "" {
					req.IMAPPassword = a.IMAPPassword
				}
				if req.SMTPPassword != "" && !storage.IsEncrypted(req.SMTPPassword) {
					enc, _ := storage.Encrypt(req.SMTPPassword)
					req.SMTPPassword = enc
				} else if req.SMTPPassword == "" {
					req.SMTPPassword = a.SMTPPassword
				}
				accounts[i] = &req
				s.saveEmailAccounts(user, accounts)
				writeJSON(w, http.StatusOK, maskEmailAccount(&req))
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodDelete:
		for i, a := range accounts {
			if a.ID == id {
				accounts = append(accounts[:i], accounts[i+1:]...)
				s.saveEmailAccounts(user, accounts)
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEmailAccountTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req EmailAccount
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	password := req.IMAPPassword
	if storage.IsEncrypted(password) {
		dec, err := storage.Decrypt(password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "decrypt failed"})
			return
		}
		password = dec
	}
	cfg := email.AccountConfig{
		IMAPHost: req.IMAPHost, IMAPPort: req.IMAPPort,
		IMAPUser: req.IMAPUser, IMAPPassword: password,
		IMAPStartTLS: req.IMAPStartTLS,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type testResult struct {
		ok  bool
		err string
	}
	done := make(chan testResult, 1)
	go func() {
		client, err := email.Connect(cfg)
		if err != nil {
			done <- testResult{false, err.Error()}
			return
		}
		defer client.Close()
		if _, err := client.ListFolders(context.Background()); err != nil {
			done <- testResult{false, err.Error()}
			return
		}
		done <- testResult{true, ""}
	}()
	select {
	case res := <-done:
		if !res.ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": res.err})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case <-ctx.Done():
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connection timed out"})
	}
}

func (s *Server) handleEmailStyle(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	file := s.emailStyleFile(user)
	switch r.Method {
	case http.MethodGet:
		var style map[string]any
		if err := storage.ReadJSON(file, &style); err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, map[string]any{})
				return
			}
		}
		if style == nil {
			style = map[string]any{}
		}
		writeJSON(w, http.StatusOK, style)
	case http.MethodPost, http.MethodPut:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		storage.WriteJSON(file, req)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

