package server

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/storage"
)

func (s *Server) registerPersonalRoutes() {
	s.mux.HandleFunc("/api/personal", s.withAuth(s.handlePersonal))
	s.mux.HandleFunc("/api/personal/add_directory", s.withAuth(s.handlePersonalAddDirectory))
	s.mux.HandleFunc("/api/personal/reload", s.withAuth(s.handlePersonalReload))
	s.mux.HandleFunc("/api/personal/upload", s.withAuth(s.handlePersonalUpload))
	s.mux.HandleFunc("/api/personal/file", s.withAuth(s.handlePersonalFile))
	s.mux.HandleFunc("/api/personal/remove_directory", s.withAuth(s.handlePersonalRemoveDirectory))
	s.mux.HandleFunc("/api/personal/", s.withAuth(s.handlePersonalOps))
}

func (s *Server) personalDirsFile(user string) string {
	return s.cfg.DataDir + "/personal_dirs_" + sanitizeFilename(user) + ".json"
}

func (s *Server) personalDataDir(user string) string {
	return filepath.Join(s.cfg.DataDir, "personal_"+sanitizeFilename(user))
}

func (s *Server) loadPersonalDirs(user string) []string {
	var dirs []string
	storage.ReadJSON(s.personalDirsFile(user), &dirs)
	if dirs == nil {
		dirs = []string{}
	}
	return dirs
}

func (s *Server) savePersonalDirs(user string, dirs []string) error {
	return storage.WriteJSON(s.personalDirsFile(user), dirs)
}

func (s *Server) handlePersonal(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	dirs := s.loadPersonalDirs(user)

	// Enumerate uploaded files from personal data dir
	type fileInfo struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	var files []fileInfo
	dataDir := s.personalDataDir(user)
	if entries, err := os.ReadDir(dataDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			files = append(files, fileInfo{
				Name: e.Name(),
				Path: filepath.Join(dataDir, e.Name()),
				Size: size,
			})
		}
	}
	if files == nil {
		files = []fileInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"directories": dirs, "files": files})
}

func (s *Server) handlePersonalAddDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	var req struct {
		Directory string `json:"directory"`
	}
	if err := decodeBody(r, &req); err != nil || req.Directory == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "directory required"})
		return
	}
	abs, err := filepath.Abs(req.Directory)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "directory not found"})
		return
	}
	dirs := s.loadPersonalDirs(user)
	for _, d := range dirs {
		if d == abs {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "directory": abs})
			return
		}
	}
	dirs = append(dirs, abs)
	s.savePersonalDirs(user, dirs)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "directory": abs})
}

func (s *Server) handlePersonalReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	dirs := s.loadPersonalDirs(user)
	indexed := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			s.memMgr.Add(context.Background(), string(data), "document", "personal", user, "")
			indexed++
		}
	}
	// Also index personal upload dir
	uploadDir := s.personalDataDir(user)
	if entries, err := os.ReadDir(uploadDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(uploadDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			s.memMgr.Add(context.Background(), string(data), "document", "personal", user, "")
			indexed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "indexed": indexed})
}

func (s *Server) handlePersonalUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file"})
		return
	}
	defer file.Close()

	dir := s.personalDataDir(user)
	os.MkdirAll(dir, 0755)

	destPath := filepath.Join(dir, filepath.Base(header.Filename))
	// Traversal guard
	if !strings.HasPrefix(destPath, dir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.memMgr.Add(context.Background(), string(data), "document", "personal", user, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "filename": header.Filename})
}

func (s *Server) handlePersonalFile(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	fp := r.URL.Query().Get("filepath")
	if fp == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "filepath required"})
		return
	}
	base := s.personalDataDir(user)
	abs := filepath.Join(base, filepath.Base(fp))
	if !strings.HasPrefix(abs, base+string(filepath.Separator)) && abs != base {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		http.ServeFile(w, r, abs)
	case http.MethodDelete:
		if err := os.Remove(abs); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePersonalRemoveDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	dir := r.URL.Query().Get("directory")
	if dir == "" {
		var req struct{ Directory string `json:"directory"` }
		decodeBody(r, &req)
		dir = req.Directory
	}
	dirs := s.loadPersonalDirs(user)
	updated := dirs[:0]
	for _, d := range dirs {
		if d != dir {
			updated = append(updated, d)
		}
	}
	s.savePersonalDirs(user, updated)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePersonalOps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
