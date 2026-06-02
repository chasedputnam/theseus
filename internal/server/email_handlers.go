package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/email"
	"github.com/chaseputnam/theseus/internal/storage"
	imap "github.com/emersion/go-imap/v2"
	"github.com/google/uuid"
)

// emailConfigForRequest resolves the email AccountConfig for the current request.
// If ?account= is set it loads that per-user account and decrypts its password,
// otherwise it falls back to the global settings-based config.
func (s *Server) emailConfigForRequest(r *http.Request, user string) (email.AccountConfig, error) {
	accountID := r.URL.Query().Get("account")
	if accountID != "" {
		for _, a := range s.loadEmailAccounts(user) {
			if a.ID != accountID {
				continue
			}
			pass := a.IMAPPassword
			if storage.IsEncrypted(pass) {
				dec, err := storage.Decrypt(pass)
				if err != nil {
					return email.AccountConfig{}, fmt.Errorf("decrypt imap password: %w", err)
				}
				pass = dec
			}
			smtpPass := a.SMTPPassword
			if storage.IsEncrypted(smtpPass) {
				if dec, err := storage.Decrypt(smtpPass); err == nil {
					smtpPass = dec
				}
			}
			return email.AccountConfig{
				IMAPHost:     a.IMAPHost,
				IMAPPort:     a.IMAPPort,
				IMAPUser:     a.IMAPUser,
				IMAPPassword: pass,
				IMAPStartTLS: a.IMAPStartTLS,
				SMTPHost:     a.SMTPHost,
				SMTPPort:     a.SMTPPort,
				SMTPUser:     a.SMTPUser,
				SMTPPassword: smtpPass,
				FromAddress:  a.FromAddress,
			}, nil
		}
		return email.AccountConfig{}, fmt.Errorf("account not found")
	}
	return s.emailConfig(), nil
}

// uidFromPath extracts a uint32 UID from the trailing path segment after prefix.
func uidFromPath(path, prefix string) (uint32, error) {
	seg := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
	n, err := strconv.ParseUint(seg, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid uid %q", seg)
	}
	return uint32(n), nil
}

func (s *Server) handleEmailList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "email not configured"})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, e := strconv.Atoi(o); e == nil && n >= 0 {
			offset = n
		}
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"

	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()

	msgs, err := client.ListMessages(context.Background(), folder, limit+offset, unreadOnly)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []*email.Message{}
	}
	total := len(msgs)
	if offset < len(msgs) {
		msgs = msgs[offset:]
	} else {
		msgs = []*email.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "total": total})
}

func (s *Server) handleEmailReadByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/read/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	msg, err := client.FetchMessage(context.Background(), folder, uid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handleEmailMarkReadByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/mark-read/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.MarkRead(context.Background(), folder, []imap.UID{imap.UID(uid)}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailMarkUnreadByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/mark-unread/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.MarkUnread(context.Background(), folder, []imap.UID{imap.UID(uid)}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailMarkAnsweredByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/mark-answered/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.SetFlag(context.Background(), folder, []imap.UID{imap.UID(uid)}, imap.Flag("\\Answered"), true); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailClearAnsweredByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/clear-answered/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.SetFlag(context.Background(), folder, []imap.UID{imap.UID(uid)}, imap.Flag("\\Answered"), false); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailArchiveByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/archive/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.MoveMessages(context.Background(), folder, "Archive", []imap.UID{imap.UID(uid)}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailDeleteByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/delete/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.MoveMessages(context.Background(), folder, "Trash", []imap.UID{imap.UID(uid)}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailDeletePermanentByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/delete-permanent/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := client.DeleteMessages(context.Background(), folder, []imap.UID{imap.UID(uid)}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailMoveByUID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/move/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	dest := r.URL.Query().Get("dest")
	if dest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dest required"})
		return
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
	if err := client.MoveMessages(context.Background(), folder, dest, []imap.UID{imap.UID(uid)}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailUnflagSpam(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	uid, err := uidFromPath(r.URL.Path, "/api/email/unflag-spam/")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "Junk"
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
	if err := client.SetFlag(context.Background(), folder, []imap.UID{imap.UID(uid)}, imap.FlagJunk, false); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEmailSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q required"})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil && n > 0 {
			limit = n
		}
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
	msgs, err := client.SearchMessages(context.Background(), folder, q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []*email.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "total": len(msgs)})
}

func (s *Server) handleEmailReminders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	cfg, err := s.emailConfigForRequest(r, user)
	if err != nil || cfg.IMAPHost == "" {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []*email.Message{}})
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	client, err := email.Connect(cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer client.Close()
	msgs, err := client.SearchMessages(context.Background(), folder, "reminder", 100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []*email.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) handleEmailAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	seg := strings.TrimPrefix(r.URL.Path, "/api/email/attachment/")
	parts := strings.SplitN(seg, "/", 2)
	if len(parts) != 2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path: expected uid/index"})
		return
	}
	uidN, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid uid"})
		return
	}
	idxN, err := strconv.Atoi(strings.TrimSuffix(parts[1], "/"))
	if err != nil {
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
	att, err := client.FetchAttachment(context.Background(), folder, uint32(uidN), idxN)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	ct := att.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, att.Filename))
	w.WriteHeader(http.StatusOK)
	w.Write(att.Data)
}

func (s *Server) handleEmailComposeUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()

	dir := filepath.Join(s.cfg.DataDir, "email_compose_"+sanitizeFilename(user))
	if err := os.MkdirAll(dir, 0700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "mkdir failed"})
		return
	}
	token := uuid.New().String()
	dest := filepath.Join(dir, token+filepath.Ext(header.Filename))
	f, err := os.Create(dest)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create failed"})
		return
	}
	defer f.Close()
	if _, err := io.Copy(f, file); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":    token,
		"filename": header.Filename,
		"path":     dest,
	})
}
