package server

// compat_routes.go — API compatibility shims for the Odysseus frontend.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/search"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

func (s *Server) registerCompatRoutes() {
	// Session singular aliases
	s.mux.HandleFunc("/api/session", s.withAuth(s.handleSessionCompat))
	s.mux.HandleFunc("/api/session/", s.withAuth(s.handleSessionByIDCompat))

	// Chat stream alias + stop/status stubs
	s.mux.HandleFunc("/api/chat/stop/", s.withAuth(s.handleChatStop))
	s.mux.HandleFunc("/api/chat/stream_status/", s.withAuth(s.handleChatStreamStatus))

	// Models
	s.mux.HandleFunc("/api/models", s.withAuth(s.handleModels))
	s.mux.HandleFunc("/api/default-chat", s.withAuth(s.handleDefaultChat))
	s.mux.HandleFunc("/api/model-endpoints/probe-local", s.withAuth(s.handleProbeLocal))

	// History alias
	s.mux.HandleFunc("/api/history/", s.withAuth(s.handleHistory))

	// AI name stub
	s.mux.HandleFunc("/api/ai/name", s.withAuth(s.handleAIName))

	// Search
	s.mux.HandleFunc("/api/search", s.withAuth(s.handleSearch))
	s.mux.HandleFunc("/api/search/query", s.withAuth(s.handleSearchQuery))
	s.mux.HandleFunc("/api/search/providers", s.withAuth(s.handleSearchProviders))

	// Upload stubs
	s.mux.HandleFunc("/api/upload", s.withAuth(s.handleUpload))
	s.mux.HandleFunc("/api/upload/", s.withAuth(s.handleUploadByID))

	// Personal docs — handled by personal_routes.go
	// Sessions extras
	s.mux.HandleFunc("/api/sessions/archived", s.withAuth(s.handleSessionsArchived))
	s.mux.HandleFunc("/api/sessions/auto-sort", s.withAuth(s.handleSessionsAutoSort))

	// Misc stubs
	s.mux.HandleFunc("/api/ping", s.handlePing)
	s.mux.HandleFunc("/api/db/stats", s.withAuth(s.handleDBStats))
	s.mux.HandleFunc("/api/rewrite", s.withAuth(s.handleRewrite))
	s.mux.HandleFunc("/api/providers", s.withAuth(s.handleProviders))
	s.mux.HandleFunc("/api/discover", s.withAuth(s.handleDiscover))
	s.mux.HandleFunc("/api/probe", s.withAuth(s.handleProbe))
	s.mux.HandleFunc("/api/tools", s.withAuth(s.handleTools))
	s.mux.HandleFunc("/api/emoji", s.withAuth(s.handleEmoji))
	s.mux.HandleFunc("/api/emoji/", s.withAuth(s.handleEmoji))
	s.mux.HandleFunc("/api/fonts/custom", s.withAuth(s.handleFontsCustom))
}

// --- /api/session (singular) ---

func (s *Server) handleSessionCompat(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if r.Method != http.MethodPost {
		s.handleSessions(w, r)
		return
	}
	r.ParseMultipartForm(1 << 20)
	name := r.FormValue("name")
	endpointURL := r.FormValue("endpoint_url")
	model := r.FormValue("model")
	rag := r.FormValue("rag") == "true"
	endpointID := r.FormValue("endpoint_id")
	skipValidation := r.FormValue("skip_validation") == "true"

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
	if endpointURL == "" && !skipValidation {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint_url is required"})
		return
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
	if err := s.db.CreateSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": sess.ID, "name": sess.Name, "model": sess.Model,
		"rag": sess.RAG, "archived": false,
	})
}

func (s *Server) handleSessionByIDCompat(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = strings.Replace(r.URL.Path, "/api/session/", "/api/sessions/", 1)
	s.handleSessionByID(w, r)
}

func (s *Server) handleChatStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Session stop is a client-side signal; no server-side stopped flag in schema.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleChatStreamStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
}

// --- /api/models ---

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	isAdmin := s.auth.IsAdmin(user)
	endpoints, err := s.db.ListModelEndpoints(user, isAdmin)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := []map[string]any{}
	for _, ep := range endpoints {
		if !ep.IsEnabled {
			continue
		}
		var models []string
		if ep.CachedModels.Valid && ep.CachedModels.String != "" {
			json.Unmarshal([]byte(ep.CachedModels.String), &models)
		}
		baseURL := llm.NormalizeBaseURL(ep.BaseURL)
		displayNames := make([]string, len(models))
		for i, m := range models {
			parts := strings.Split(m, "/")
			displayNames[i] = parts[len(parts)-1]
		}
		item := map[string]any{
			"host": "custom", "port": 0,
			"url": baseURL, "endpoint_url": baseURL, "base_url": ep.BaseURL,
			"models": models, "models_display": displayNames,
			"models_extra": []string{}, "models_extra_display": []string{},
			"endpoint_id": ep.ID, "endpoint_name": ep.Name,
			"model_type": ep.ModelType, "supports_tools": ep.SupportsTools.Bool,
		}
		if len(models) == 0 {
			item["offline"] = true
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": []any{}, "items": items})
}

// --- /api/default-chat ---

func (s *Server) handleDefaultChat(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	isAdmin := s.auth.IsAdmin(user)

	prefsFile := fmt.Sprintf("%s/prefs_%s.json", s.cfg.DataDir, sanitizeFilename(user))
	var prefs map[string]any
	storage.ReadJSON(prefsFile, &prefs)

	epID, model := "", ""
	if prefs != nil {
		epID, _ = prefs["default_endpoint_id"].(string)
		model, _ = prefs["default_model"].(string)
	}
	if epID == "" {
		epID = settings.GetString("default_endpoint_id")
		model = settings.GetString("default_model")
	}

	endpoints, _ := s.db.ListModelEndpoints(user, isAdmin)
	var ep *db.ModelEndpoint
	for _, e := range endpoints {
		if !e.IsEnabled {
			continue
		}
		if epID == "" || e.ID == epID {
			ep = e
			if epID != "" {
				break
			}
		}
	}
	if ep == nil {
		writeJSON(w, http.StatusOK, map[string]string{"endpoint_id": "", "endpoint_url": "", "model": ""})
		return
	}
	if model == "" && ep.CachedModels.Valid {
		var models []string
		if json.Unmarshal([]byte(ep.CachedModels.String), &models) == nil && len(models) > 0 {
			model = models[0]
		}
	}
	baseURL := llm.NormalizeBaseURL(ep.BaseURL)
	writeJSON(w, http.StatusOK, map[string]string{
		"endpoint_id": ep.ID, "endpoint_url": baseURL, "model": model,
	})
}

// --- /api/model-endpoints/probe-local ---

func (s *Server) handleProbeLocal(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	isAdmin := s.auth.IsAdmin(user)
	endpoints, _ := s.db.ListModelEndpoints(user, isAdmin)
	result := map[string]any{}
	client := llm.New()
	for _, ep := range endpoints {
		if !ep.IsEnabled {
			continue
		}
		headers := map[string]string{}
		if ep.APIKey.Valid && ep.APIKey.String != "" && ep.APIKey.String != "stored" {
			headers["Authorization"] = "Bearer " + ep.APIKey.String
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		models, err := client.DiscoverModels(ctx, ep.BaseURL, headers)
		cancel()
		if err != nil {
			result[ep.ID] = map[string]any{"online": false, "models": []string{}}
		} else {
			result[ep.ID] = map[string]any{"online": true, "models": models}
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// --- /api/history/{id} ---

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	sid := strings.TrimPrefix(r.URL.Path, "/api/history/")
	sess, err := s.db.GetSession(sid)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if sess.Owner.Valid && sess.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	msgs, _ := s.db.ListMessages(sid)
	history := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, map[string]any{"role": m.Role, "content": m.Content})
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

// --- /api/ai/name ---

func (s *Server) handleAIName(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	prefsFile := fmt.Sprintf("%s/prefs_%s.json", s.cfg.DataDir, sanitizeFilename(user))
	if r.Method == http.MethodPost {
		var req struct{ Name string `json:"name"` }
		json.NewDecoder(r.Body).Decode(&req)
		var prefs map[string]any
		storage.ReadJSON(prefsFile, &prefs)
		if prefs == nil {
			prefs = map[string]any{}
		}
		prefs["ai_name"] = req.Name
		storage.WriteJSON(prefsFile, prefs)
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}
	var prefs map[string]any
	storage.ReadJSON(prefsFile, &prefs)
	name := "Theseus"
	if prefs != nil {
		if n, ok := prefs["ai_name"].(string); ok && n != "" {
			name = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// --- /api/search ---

func (s *Server) searchClient() *search.Client {
	return search.BuildFromSettings(
		settings.GetString("search_provider"),
		settings.GetString("search_url"),
		[]string{"duckduckgo"},
		settings.GetString("brave_api_key"),
		settings.GetString("google_pse_key"),
		settings.GetString("google_pse_cx"),
		settings.GetString("tavily_api_key"),
		settings.GetString("serper_api_key"),
	)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		r.ParseForm()
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		q = r.FormValue("q")
	}
	if q == "" {
		q = r.FormValue("query")
	}
	if q == "" && r.Method == http.MethodPost {
		var body struct {
			Query string `json:"query"`
			Q     string `json:"q"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Query != "" {
			q = body.Query
		} else {
			q = body.Q
		}
	}
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query required"})
		return
	}
	// Allow per-request provider override (used by settings test)
	provider := r.FormValue("provider")
	countStr := r.FormValue("count")
	count := 5
	if countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 {
			count = n
		}
	}
	var client *search.Client
	if provider != "" && provider != settings.GetString("search_provider") {
		client = search.BuildFromSettings(
			provider,
			settings.GetString("search_url"),
			[]string{"duckduckgo"},
			settings.GetString("brave_api_key"),
			settings.GetString("google_pse_key"),
			settings.GetString("google_pse_cx"),
			settings.GetString("tavily_api_key"),
			settings.GetString("serper_api_key"),
		)
	} else {
		client = s.searchClient()
	}
	results, err := client.Search(r.Context(), q, count)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleSearchQuery(w http.ResponseWriter, r *http.Request) {
	s.handleSearch(w, r)
}

func (s *Server) handleSearchProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": []string{"searxng", "duckduckgo", "brave", "google_pse", "tavily", "serper"},
		"current":   settings.GetString("search_provider"),
	})
}

// --- /api/upload ---

type UploadMeta struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	Owner       string    `json:"owner"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Server) uploadsMetaFile(user string) string {
	return s.cfg.DataDir + "/uploads_" + sanitizeFilename(user) + ".json"
}

func (s *Server) loadUploadsMeta(user string) []*UploadMeta {
	var meta []*UploadMeta
	storage.ReadJSON(s.uploadsMetaFile(user), &meta)
	if meta == nil {
		meta = []*UploadMeta{}
	}
	return meta
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"files": s.loadUploadsMeta(user)})
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			status := http.StatusBadRequest
			if err.Error() == "http: request body too large" {
				status = http.StatusRequestEntityTooLarge
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file"})
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if s.totalUploadSize(user)+int64(len(data)) > uploadQuotaBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "upload quota exceeded"})
			return
		}
		uploadDir := filepath.Join(s.cfg.DataDir, "uploads")
		os.MkdirAll(uploadDir, 0755)
		id := uuid.New().String()
		ext := filepath.Ext(header.Filename)
		destPath := filepath.Join(uploadDir, id+ext)
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ct := header.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		meta := &UploadMeta{
			ID: id, Filename: header.Filename,
			Size: int64(len(data)), ContentType: ct,
			Owner: user, CreatedAt: time.Now().UTC(),
		}
		all := s.loadUploadsMeta(user)
		all = append(all, meta)
		storage.WriteJSON(s.uploadsMetaFile(user), all)
		writeJSON(w, http.StatusOK, meta)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUploadByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	path := strings.TrimPrefix(r.URL.Path, "/api/upload/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	all := s.loadUploadsMeta(user)
	var found *UploadMeta
	for _, m := range all {
		if m.ID == id {
			found = m
			break
		}
	}
	if found == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	ext := filepath.Ext(found.Filename)
	filePath := filepath.Join(s.cfg.DataDir, "uploads", id+ext)
	// Handle /vision sub-action
	if action == "vision" {
		visionFile := filepath.Join(s.cfg.DataDir, "uploads", id+".vision.txt")
		switch r.Method {
		case http.MethodGet:
			text, _ := os.ReadFile(visionFile)
			writeJSON(w, http.StatusOK, map[string]string{"text": string(text)})
		case http.MethodPut:
			var req struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			os.WriteFile(visionFile, []byte(req.Text), 0644)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		f, err := os.Open(filePath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", found.ContentType)
		http.ServeContent(w, r, found.Filename, found.CreatedAt, f)
	case http.MethodDelete:
		os.Remove(filePath)
		newAll := make([]*UploadMeta, 0, len(all)-1)
		for _, m := range all {
			if m.ID != id {
				newAll = append(newAll, m)
			}
		}
		storage.WriteJSON(s.uploadsMetaFile(user), newAll)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- /api/sessions/archived and /api/sessions/auto-sort ---

func (s *Server) handleSessionsArchived(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	sessions, err := s.db.ListSessions(user, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type sessionResponse struct {
		*db.Session
		HasDocuments bool   `json:"has_documents"`
		HasImages    bool   `json:"has_images"`
		IsOpenClaw   bool   `json:"is_openclaw"`
		EndpointID   string `json:"endpoint_id"`
	}
	epURLToID := s.buildEndpointURLMap(user)
	archived := []sessionResponse{}
	for _, sess := range sessions {
		if !sess.Archived {
			continue
		}
		hasDocs, _ := s.db.SessionHasDocuments(sess.ID)
		hasImgs, _ := s.db.SessionHasImages(sess.ID)
		isOC := sess.Mode.Valid && sess.Mode.String == "openclaw"
		archived = append(archived, sessionResponse{
			Session:      sess,
			HasDocuments: hasDocs,
			HasImages:    hasImgs,
			IsOpenClaw:   isOC,
			EndpointID:   epURLToID[sess.EndpointURL],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": archived, "total": len(archived)})
}

func (s *Server) handleSessionsAutoSort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	sessions, err := s.db.ListSessions(user, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var failed int
	for i, sess := range sessions {
		if err := s.db.UpdateSessionSortOrder(sess.ID, i); err != nil {
			log.Printf("auto-sort: update session %s: %v", sess.ID, err)
			failed++
		}
	}
	if failed > 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "partial failure", "failed": failed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "sorted": len(sessions)})
}

// --- Misc stubs ---

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `{"pong":true}`)
}

func (s *Server) handleDBStats(w http.ResponseWriter, r *http.Request) {
	stats := s.db.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"open_connections": stats.OpenConnections,
		"in_use":           stats.InUse,
		"idle":             stats.Idle,
	})
}

func (s *Server) handleRewrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text  string `json:"text"`
		Style string `json:"style"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	style := req.Style
	if style == "" {
		style = "improve"
	}
	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	prompt := "Rewrite the following text to " + style + " it. Return only the rewritten text.\n\n" + req.Text
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
	writeJSON(w, http.StatusOK, map[string]string{"result": result.String()})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"providers": []any{}})
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	type endpoint struct {
		URL    string   `json:"url"`
		Name   string   `json:"name"`
		Models []string `json:"models"`
		Online bool     `json:"online"`
	}
	ports := []struct{ port int; name string }{
		{11434, "Ollama"},
		{1234, "LM Studio"},
		{8080, "llama.cpp"},
		{8000, "Generic"},
	}
	client := llm.New()
	type probeResult struct {
		ep  endpoint
		idx int
	}
	ch := make(chan probeResult, len(ports))
	for i, p := range ports {
		go func(idx, port int, name string) {
			url := fmt.Sprintf("http://localhost:%d", port)
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			models, err := client.DiscoverModels(ctx, url, nil)
			if err != nil {
				ch <- probeResult{ep: endpoint{URL: url, Name: name, Models: []string{}, Online: false}, idx: idx}
				return
			}
			ch <- probeResult{ep: endpoint{URL: url, Name: name, Models: models, Online: true}, idx: idx}
		}(i, p.port, p.name)
	}
	collected := make([]endpoint, len(ports))
	for range ports {
		r := <-ch
		collected[r.idx] = r.ep
	}
	results := make([]endpoint, 0, len(ports))
	for _, ep := range collected {
		if ep.Online {
			results = append(results, ep)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoints": results})
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if r.Method == http.MethodPost {
		json.NewDecoder(r.Body).Decode(&req)
	} else {
		req.URL = r.URL.Query().Get("url")
	}
	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url required"})
		return
	}
	client := llm.New()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	models, err := client.DiscoverModels(ctx, req.URL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"online": false, "models": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": true, "models": models})
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	builtins := []map[string]any{
		{"name": "bash", "description": "Execute shell commands", "enabled": true},
		{"name": "python", "description": "Execute Python code", "enabled": true},
		{"name": "files", "description": "Read and write files", "enabled": true},
		{"name": "documents", "description": "Create and edit documents with FIND/REPLACE", "enabled": true},
		{"name": "web_search", "description": "Search the web", "enabled": true},
		{"name": "memory", "description": "Store and retrieve memories", "enabled": true},
	}
	for _, t := range s.mcpManager.ListTools() {
		builtins = append(builtins, map[string]any{
			"name": t.Name, "description": t.Description, "enabled": true,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": builtins})
}

func (s *Server) handleEmoji(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/emoji/")
	if strings.HasSuffix(path, ".svg") {
		code := strings.TrimSuffix(path, ".svg")
		svgPath := filepath.Join(s.cfg.StaticDir, "emoji", code+".svg")
		if _, err := os.Stat(svgPath); err == nil {
			w.Header().Set("Content-Type", "image/svg+xml")
			http.ServeFile(w, r, svgPath)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>`)) 
		return
	}
	emojiDir := filepath.Join(s.cfg.StaticDir, "emoji")
	entries, err := os.ReadDir(emojiDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"emoji": []any{}})
		return
	}
	codes := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".svg") {
			codes = append(codes, strings.TrimSuffix(e.Name(), ".svg"))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"emoji": codes})
}

func (s *Server) handleFontsCustom(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"fonts": []any{}})
}
