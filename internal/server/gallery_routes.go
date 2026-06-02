package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/google/uuid"
	"github.com/rwcarlsen/goexif/exif"
)

func (s *Server) registerGalleryRoutes() {
	s.mux.HandleFunc("/api/gallery", s.withAuth(s.handleGallery))
	s.mux.HandleFunc("/api/gallery/upload", s.withAuth(s.handleGalleryUpload))
	s.mux.HandleFunc("/api/gallery/library", s.withAuth(s.handleGalleryLibrary))
	s.mux.HandleFunc("/api/gallery/style-transfer", s.withAuth(s.handleGalleryStyleTransfer))
	s.mux.HandleFunc("/api/gallery/albums", s.withAuth(s.handleGalleryAlbums))
	s.mux.HandleFunc("/api/gallery/albums/", s.withAuth(s.handleGalleryAlbumByID))
	s.mux.HandleFunc("/api/gallery/", s.withAuth(s.handleGalleryImageByID))
	s.mux.HandleFunc("/api/editor-drafts", s.withAuth(s.handleEditorDrafts))
	s.mux.HandleFunc("/api/editor-drafts/", s.withAuth(s.handleEditorDraftByID))
}

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	albumID := r.URL.Query().Get("album_id")
	images, err := s.db.ListGalleryImages(user, albumID, 100, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if images == nil {
		images = []*db.GalleryImage{}
	}
	writeJSON(w, http.StatusOK, images)
}

func (s *Server) handleGalleryUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	r.ParseMultipartForm(50 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file"})
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Dedup by SHA-256
	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	existing, err := s.db.GetGalleryImageByHash(hash, user)
	if err == nil && existing != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "duplicate": true,
			"filename": existing.Filename, "id": existing.ID,
		})
		return
	}

	// Save file
	imgDir := filepath.Join(s.cfg.DataDir, "generated_images")
	os.MkdirAll(imgDir, 0755)
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".jpg"
	}
	filename := uuid.New().String() + ext
	if err := os.WriteFile(filepath.Join(imgDir, filename), content, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	img := &db.GalleryImage{
		ID:       uuid.New().String(),
		Filename: filename,
		Prompt:   "",
		Tags:     "",
		AITags:   "",
		Owner:    sql.NullString{String: user, Valid: user != ""},
		IsActive: true,
		FileHash: sql.NullString{String: hash, Valid: true},
		FileSize: sql.NullInt64{Int64: int64(len(content)), Valid: true},
	}

	// Extract EXIF
	extractEXIF(img, content)

	albumID := r.FormValue("album_id")
	if albumID != "" {
		img.AlbumID = sql.NullString{String: albumID, Valid: true}
	}

	if err := s.db.CreateGalleryImage(img); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": img.ID, "filename": filename})
}

func (s *Server) handleGalleryImageByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/gallery/"), "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "replace" && r.Method == http.MethodPost {
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form"})
			return
		}
		file, header, err := r.FormFile("image")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image required"})
			return
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		imgDir := filepath.Join(s.cfg.DataDir, "generated_images")
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext == "" {
			ext = ".jpg"
		}
		filename := uuid.New().String() + ext
		if err := os.WriteFile(filepath.Join(imgDir, filename), content, 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		img, err := s.db.GetGalleryImage(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		img.Filename = filename
		if err := s.db.UpdateGalleryImage(img); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "filename": filename, "id": id})
		return
	}

	img, err := s.db.GetGalleryImage(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if img.Owner.Valid && img.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, img)
	case http.MethodPatch, http.MethodPut:
		var req struct {
			Tags     *string `json:"tags"`
			Prompt   *string `json:"prompt"`
			Favorite *bool   `json:"favorite"`
			AlbumID  *string `json:"album_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Tags != nil {
			img.Tags = *req.Tags
		}
		if req.Prompt != nil {
			img.Prompt = *req.Prompt
		}
		if req.Favorite != nil {
			img.Favorite = *req.Favorite
		}
		if req.AlbumID != nil {
			img.AlbumID = sql.NullString{String: *req.AlbumID, Valid: *req.AlbumID != ""}
		}
		if err := s.db.UpdateGalleryImage(img); err != nil {
			log.Printf("db: UpdateGalleryImage: %v", err)
		}
		writeJSON(w, http.StatusOK, img)
	case http.MethodDelete:
		if err := s.db.DeleteGalleryImage(id); err != nil {
			log.Printf("db: DeleteGalleryImage: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGalleryAlbums(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		albums, _ := s.db.ListGalleryAlbums(user)
		if albums == nil {
			albums = []*db.GalleryAlbum{}
		}
		writeJSON(w, http.StatusOK, albums)
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		album := &db.GalleryAlbum{
			ID:          uuid.New().String(),
			Name:        req.Name,
			Description: req.Description,
			Owner:       sql.NullString{String: user, Valid: user != ""},
		}
		if err := s.db.CreateGalleryAlbum(album); err != nil {
			log.Printf("db: CreateGalleryAlbum: %v", err)
		}
		writeJSON(w, http.StatusOK, album)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGalleryAlbumByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/gallery/albums/")
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Name    *string `json:"name"`
			CoverID *string `json:"cover_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		albums, _ := s.db.ListGalleryAlbums("")
		for _, a := range albums {
			if a.ID == id {
				if req.Name != nil {
					a.Name = *req.Name
				}
				if req.CoverID != nil {
					a.CoverID = sql.NullString{String: *req.CoverID, Valid: *req.CoverID != ""}
				}
				if err := s.db.UpdateGalleryAlbum(a); err != nil {
					log.Printf("db: UpdateGalleryAlbum: %v", err)
				}
				writeJSON(w, http.StatusOK, a)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodDelete:
		if err := s.db.DeleteGalleryAlbum(id); err != nil {
			log.Printf("db: DeleteGalleryAlbum: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGalleryLibrary(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	images, err := s.db.ListGalleryImages(user, "", 500, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if images == nil {
		images = []*db.GalleryImage{}
	}
	albums, _ := s.db.ListGalleryAlbums(user)
	if albums == nil {
		albums = []*db.GalleryAlbum{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": images, "albums": albums, "total": len(images)})
}

func (s *Server) handleGalleryStyleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form"})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image required"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	imgDir := filepath.Join(s.cfg.DataDir, "generated_images")
	os.MkdirAll(imgDir, 0755)
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".jpg"
	}
	filename := uuid.New().String() + ext
	if err := os.WriteFile(filepath.Join(imgDir, filename), content, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	prompt := r.FormValue("prompt")
	img := &db.GalleryImage{
		ID:       uuid.New().String(),
		Filename: filename,
		Prompt:   prompt,
		Tags:     "style-transfer",
		Owner:    sql.NullString{String: user, Valid: user != ""},
		IsActive: true,
	}
	if err := s.db.CreateGalleryImage(img); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": img.ID, "filename": filename})
}

func (s *Server) handleEditorDrafts(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		drafts, _ := s.db.ListEditorDrafts(user)
		if drafts == nil {
			drafts = []*db.EditorDraft{}
		}
		writeJSON(w, http.StatusOK, drafts)
	case http.MethodPost:
		var req struct {
			Name          string `json:"name"`
			SourceImageID string `json:"source_image_id"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			Payload       string `json:"payload"`
			Thumbnail     string `json:"thumbnail"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		draft := &db.EditorDraft{
			ID:            uuid.New().String(),
			Owner:         sql.NullString{String: user, Valid: user != ""},
			Name:          req.Name,
			SourceImageID: sql.NullString{String: req.SourceImageID, Valid: req.SourceImageID != ""},
			Width:         sql.NullInt64{Int64: int64(req.Width), Valid: req.Width > 0},
			Height:        sql.NullInt64{Int64: int64(req.Height), Valid: req.Height > 0},
			Payload:       req.Payload,
			Thumbnail:     sql.NullString{String: req.Thumbnail, Valid: req.Thumbnail != ""},
			IsActive:      true,
		}
		if err := s.db.CreateEditorDraft(draft); err != nil {
			log.Printf("db: CreateEditorDraft: %v", err)
		}
		writeJSON(w, http.StatusOK, draft)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEditorDraftByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/editor-drafts/")
	switch r.Method {
	case http.MethodGet:
		draft, err := s.db.GetEditorDraft(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, draft)
	case http.MethodPut, http.MethodPatch:
		draft, err := s.db.GetEditorDraft(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		var req struct {
			Name      *string `json:"name"`
			Payload   *string `json:"payload"`
			Thumbnail *string `json:"thumbnail"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name != nil {
			draft.Name = *req.Name
		}
		if req.Payload != nil {
			draft.Payload = *req.Payload
		}
		if req.Thumbnail != nil {
			draft.Thumbnail = sql.NullString{String: *req.Thumbnail, Valid: true}
		}
		if err := s.db.UpdateEditorDraft(draft); err != nil {
			log.Printf("db: UpdateEditorDraft: %v", err)
		}
		writeJSON(w, http.StatusOK, draft)
	case http.MethodDelete:
		if err := s.db.DeleteEditorDraft(id); err != nil {
			log.Printf("db: DeleteEditorDraft: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func extractEXIF(img *db.GalleryImage, data []byte) {
	x, err := exif.Decode(strings.NewReader(string(data)))
	if err != nil {
		return
	}
	if t, err := x.DateTime(); err == nil {
		img.TakenAt = sql.NullTime{Time: t, Valid: true}
	}
	if make, err := x.Get(exif.Make); err == nil {
		if v, err := make.StringVal(); err == nil { img.CameraMake = sql.NullString{String: v, Valid: true} }
	}
	if model, err := x.Get(exif.Model); err == nil {
		if v, err := model.StringVal(); err == nil { img.CameraModel = sql.NullString{String: v, Valid: true} }
	}
	if lat, lng, err := x.LatLong(); err == nil {
		img.GPSLat = sql.NullString{String: fmt.Sprintf("%f", lat), Valid: true}
		img.GPSLng = sql.NullString{String: fmt.Sprintf("%f", lng), Valid: true}
	}
	if px, err := x.Get(exif.PixelXDimension); err == nil {
		if v, err := px.Int(0); err == nil {
			img.Width = sql.NullInt64{Int64: int64(v), Valid: true}
		}
	}
	if py, err := x.Get(exif.PixelYDimension); err == nil {
		if v, err := py.Int(0); err == nil {
			img.Height = sql.NullInt64{Int64: int64(v), Valid: true}
		}
	}
}
