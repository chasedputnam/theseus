package server

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/google/uuid"
)

func (s *Server) handleDocumentsTidy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	var result strings.Builder
	lc := llm.New()
	ch, err := lc.Stream(r.Context(), llm.StreamRequest{
		URL:   endpointURL,
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: "Tidy and improve the following document. Fix grammar, improve clarity, preserve meaning. Return only the improved text."},
			{Role: "user", Content: req.Content},
		},
	})
	if err == nil {
		for chunk := range ch {
			if chunk.Error == nil {
				result.WriteString(chunk.Delta)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": result.String()})
}

func (s *Server) handleDocumentsExportZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	docs, err := s.db.ListDocuments(user, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="documents.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, doc := range docs {
		name := sanitizeFilename(doc.Title) + ".md"
		f, err := zw.Create(name)
		if err != nil {
			continue
		}
		fmt.Fprint(f, doc.CurrentContent)
	}
}

func (s *Server) handleDocumentsImportPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ptPath, err := exec.LookPath("pdftotext")
	if err != nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "pdftotext not available"})
		return
	}
	user := auth.CurrentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()
	pdfData, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cmd := exec.Command(ptPath, "-", "-")
	cmd.Stdin = strings.NewReader(string(pdfData))
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "pdftotext failed: " + err.Error()})
		return
	}
	title := strings.TrimSuffix(header.Filename, ".pdf")
	if title == "" {
		title = "Imported PDF"
	}
	doc := &db.Document{
		ID:             uuid.New().String(),
		Title:          title,
		CurrentContent: string(out),
		IsActive:       true,
		Owner:          sql.NullString{String: user, Valid: true},
	}
	if err := s.db.CreateDocument(doc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleDocumentsAITidy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	docs, err := s.db.ListDocuments(user, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	tidied := 0
	for _, doc := range docs {
		var result strings.Builder
		lc := llm.New()
		ch, err := lc.Stream(r.Context(), llm.StreamRequest{
			URL:   endpointURL,
			Model: model,
			Messages: []llm.Message{
				{Role: "system", Content: "Tidy and improve the following document. Fix grammar, improve clarity, preserve meaning. Return only the improved text."},
				{Role: "user", Content: doc.CurrentContent},
			},
		})
		if err != nil {
			continue
		}
		for chunk := range ch {
			if chunk.Error == nil {
				result.WriteString(chunk.Delta)
			}
		}
		tidied++
		doc.CurrentContent = result.String()
		doc.UpdatedAt = time.Now().UTC()
		s.db.UpdateDocument(doc)
	}
	writeJSON(w, http.StatusOK, map[string]int{"tidied": tidied})
}
