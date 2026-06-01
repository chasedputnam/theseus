package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/google/uuid"
	"database/sql"
	"time"
	"github.com/chaseputnam/theseus/internal/db"
)

func (s *Server) registerDocumentRoutes() {
	s.mux.HandleFunc("/api/documents", s.withAuth(s.handleDocuments))
	s.mux.HandleFunc("/api/documents/", s.withAuth(s.handleDocumentByID))
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
