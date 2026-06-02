package server

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/google/uuid"
)

func (s *Server) registerDocumentRoutes() {
	s.mux.HandleFunc("/api/documents", s.withAuth(s.handleDocuments))
	s.mux.HandleFunc("/api/documents/library", s.withAuth(s.handleDocumentsLibrary))
	s.mux.HandleFunc("/api/documents/tidy", s.withAuth(s.handleDocumentsTidy))
	s.mux.HandleFunc("/api/documents/export-zip", s.withAuth(s.handleDocumentsExportZip))
	s.mux.HandleFunc("/api/documents/import-pdf", s.withAuth(s.handleDocumentsImportPDF))
	s.mux.HandleFunc("/api/documents/ai-tidy", s.withAuth(s.handleDocumentsAITidy))
	s.mux.HandleFunc("/api/documents/", s.withAuth(s.handleDocumentByID))
	s.mux.HandleFunc("/api/document/", s.withAuth(s.handleDocumentAlias))
}

func (s *Server) handleDocumentAlias(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	rest := strings.TrimPrefix(r.URL.Path, "/api/document/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "archive" && r.Method == http.MethodPost {
		archivedStr := r.URL.Query().Get("archived")
		archived := archivedStr != "false"
		doc, err := s.db.GetDocument(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if doc.Owner.Valid && doc.Owner.String != user && !s.auth.IsAdmin(user) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		doc.Archived = archived
		s.db.UpdateDocument(doc)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Rewrite to /api/documents/ and delegate
	r.URL.Path = "/api/documents/" + rest
	s.handleDocumentByID(w, r)
}

func (s *Server) handleDocumentsLibrary(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	archived := r.URL.Query().Get("archived") == "true"
	docs, err := s.db.ListDocuments(user, archived)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []*db.Document{}
	}
	// Apply sort
	if sort := r.URL.Query().Get("sort"); sort == "recent" {
		// already sorted by updated_at DESC from DB
	}
	total := len(docs)
	// Apply limit/offset
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if lim, err := strconv.Atoi(limitStr); err == nil && lim > 0 {
			off := 0
			if offStr := r.URL.Query().Get("offset"); offStr != "" {
				if o, err := strconv.Atoi(offStr); err == nil {
					off = o
				}
			}
			if off >= len(docs) {
				docs = []*db.Document{}
			} else {
				docs = docs[off:]
				if lim < len(docs) {
					docs = docs[:lim]
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs, "total": total})
}

func (s *Server) handleDocuments(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		archived := r.URL.Query().Get("archived") == "true"
		docs, err := s.db.ListDocuments(user, archived)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if docs == nil {
			docs = []*db.Document{}
		}
		writeJSON(w, http.StatusOK, docs)
	case http.MethodPost:
		var req struct {
			Title    string `json:"title"`
			Language string `json:"language"`
			Content  string `json:"content"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		doc := &db.Document{
			ID:             uuid.New().String(),
			Title:          req.Title,
			Language:       sql.NullString{String: req.Language, Valid: req.Language != ""},
			CurrentContent: req.Content,
			VersionCount:   1,
			IsActive:       true,
			Owner:          sql.NullString{String: user, Valid: user != ""},
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}
		if req.SessionID != "" {
			doc.SessionID = sql.NullString{String: req.SessionID, Valid: true}
		}
		if err := s.db.CreateDocument(doc); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Create initial version
		ver := &db.DocumentVersion{
			ID: uuid.New().String(), DocumentID: doc.ID,
			VersionNumber: 1, Content: req.Content, Source: "user",
			CreatedAt: time.Now().UTC(),
		}
		if err := s.db.AddDocumentVersion(ver); err != nil {
			log.Printf("db: AddDocumentVersion: %v", err)
		}
		writeJSON(w, http.StatusOK, doc)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDocumentByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/documents/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	doc, err := s.db.GetDocument(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	if doc.Owner.Valid && doc.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	switch action {
	case "versions":
		versions, err := s.db.ListDocumentVersions(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if versions == nil {
			versions = []*db.DocumentVersion{}
		}
		writeJSON(w, http.StatusOK, versions)
		return
	case "export":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+doc.Title+`.md"`)
		w.Write([]byte(doc.CurrentContent))
		return
	case "export-pdf":
		var tool string
		var toolArgs []string
		if p, err := exec.LookPath("wkhtmltopdf"); err == nil {
			tool = p
			toolArgs = []string{"-", "-"}
		} else if p, err := exec.LookPath("chromium"); err == nil {
			tool = p
			toolArgs = []string{"--headless", "--disable-gpu", "--print-to-pdf=/dev/stdout", "--no-margins", "data:text/html," + doc.CurrentContent}
		} else if p, err := exec.LookPath("chromium-browser"); err == nil {
			tool = p
			toolArgs = []string{"--headless", "--disable-gpu", "--print-to-pdf=/dev/stdout", "--no-margins", "data:text/html," + doc.CurrentContent}
		}
		if tool == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "pdf export tool not available"})
			return
		}
		cmd := exec.CommandContext(r.Context(), tool, toolArgs...)
		if tool != "" && len(toolArgs) > 0 && toolArgs[0] == "-" {
			cmd.Stdin = strings.NewReader(doc.CurrentContent)
		}
		out, err := cmd.Output()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+doc.Title+`.pdf"`)
		w.Write(out)
		return
	case "extract-pdf-text":
		ptPath, err := exec.LookPath("pdftotext")
		if err != nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "pdftotext not available"})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
		pdfData, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cmd := exec.CommandContext(r.Context(), ptPath, "-", "-")
		cmd.Stdin = strings.NewReader(string(pdfData))
		out, err := cmd.Output()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"text": string(out)})
		return
	case "tidy":
		if r.Method == http.MethodPost {
			var req struct{ Verdict string `json:"verdict"` }
			json.NewDecoder(r.Body).Decode(&req)
			doc.TidyVerdict = sql.NullString{String: req.Verdict, Valid: req.Verdict != ""}
			if err := s.db.UpdateDocument(doc); err != nil {
				log.Printf("db: UpdateDocument: %v", err)
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
		return
	default:
		// Check for version/{num} or restore/{num} patterns
		if len(parts) >= 3 {
			sub := parts[1]
			numStr := parts[2]
			num, numErr := strconv.Atoi(numStr)
			if numErr == nil {
				switch sub {
				case "version":
					ver, err := s.db.GetDocumentVersionByNumber(id, num)
					if err != nil {
						writeJSON(w, http.StatusNotFound, map[string]string{"error": "version not found"})
						return
					}
					writeJSON(w, http.StatusOK, ver)
					return
				case "restore":
					if r.Method != http.MethodPost {
						http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
						return
					}
					ver, err := s.db.GetDocumentVersionByNumber(id, num)
					if err != nil {
						writeJSON(w, http.StatusNotFound, map[string]string{"error": "version not found"})
						return
					}
					doc.CurrentContent = ver.Content
					doc.UpdatedAt = time.Now().UTC()
					if err := s.db.UpdateDocument(doc); err != nil {
						writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
						return
					}
					writeJSON(w, http.StatusOK, doc)
					return
				}
			}
		}
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, doc)
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Title    *string `json:"title"`
			Content  *string `json:"content"`
			Language *string `json:"language"`
			Archived *bool   `json:"archived"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Title != nil {
			doc.Title = *req.Title
		}
		if req.Content != nil {
			doc.CurrentContent = *req.Content
			doc.VersionCount++
			ver := &db.DocumentVersion{
				ID: uuid.New().String(), DocumentID: doc.ID,
				VersionNumber: doc.VersionCount, Content: *req.Content,
				Source: "user", CreatedAt: time.Now().UTC(),
			}
			if err := s.db.AddDocumentVersion(ver); err != nil {
				log.Printf("db: AddDocumentVersion: %v", err)
			}
		}
		if req.Language != nil {
			doc.Language = sql.NullString{String: *req.Language, Valid: *req.Language != ""}
		}
		if req.Archived != nil {
			doc.Archived = *req.Archived
		}
		doc.UpdatedAt = time.Now().UTC()
		if err := s.db.UpdateDocument(doc); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, doc)
	case http.MethodDelete:
		if err := s.db.DeleteDocument(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
