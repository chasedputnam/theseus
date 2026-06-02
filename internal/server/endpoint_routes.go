package server

import (
	"context"
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
	"github.com/google/uuid"
)

func (s *Server) registerEndpointRoutes() {
	s.mux.HandleFunc("/api/model-endpoints", s.withAuth(s.handleEndpoints))
	s.mux.HandleFunc("/api/model-endpoints/", s.withAuth(s.handleEndpointByID))
}

func (s *Server) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	isAdmin := s.auth.IsAdmin(user)

	switch r.Method {
	case http.MethodGet:
		endpoints, err := s.db.ListModelEndpoints(user, isAdmin)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if endpoints == nil {
			endpoints = []*db.ModelEndpoint{}
		}
		type epResponse struct {
			ID           string   `json:"id"`
			Name         string   `json:"name"`
			BaseURL      string   `json:"base_url"`
			IsEnabled    bool     `json:"is_enabled"`
			ModelType    string   `json:"model_type"`
			Online       bool     `json:"online"`
			HasKey       bool     `json:"has_key"`
			Models       []string `json:"models"`
			HiddenCount  int      `json:"hidden_count"`
			SupportsTools bool    `json:"supports_tools"`
		}
		result := make([]epResponse, 0, len(endpoints))
		for _, e := range endpoints {
			var allModels []string
			if e.CachedModels.Valid && e.CachedModels.String != "" {
				json.Unmarshal([]byte(e.CachedModels.String), &allModels)
			}
			var hidden []string
			if e.HiddenModels.Valid && e.HiddenModels.String != "" {
				json.Unmarshal([]byte(e.HiddenModels.String), &hidden)
			}
			hiddenSet := make(map[string]bool, len(hidden))
			for _, h := range hidden {
				hiddenSet[h] = true
			}
			visible := make([]string, 0, len(allModels))
			for _, m := range allModels {
				if !hiddenSet[m] {
					visible = append(visible, m)
				}
			}
			result = append(result, epResponse{
				ID:           e.ID,
				Name:         e.Name,
				BaseURL:      e.BaseURL,
				IsEnabled:    e.IsEnabled,
				ModelType:    e.ModelType,
				Online:       len(allModels) > 0,
				HasKey:       e.APIKey.Valid && e.APIKey.String != "",
				Models:       visible,
				HiddenCount:  len(hidden),
				SupportsTools: e.SupportsTools.Bool,
			})
		}
		writeJSON(w, http.StatusOK, result)
	case http.MethodPost:
		if !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin only"})
			return
		}
		var req struct {
			Name      string
			BaseURL   string
			APIKey    string
			ModelType string
			Owner     string
		}
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			var j struct {
				Name      string `json:"name"`
				BaseURL   string `json:"base_url"`
				APIKey    string `json:"api_key"`
				ModelType string `json:"model_type"`
				Owner     string `json:"owner"`
			}
			if err := json.NewDecoder(r.Body).Decode(&j); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			req.Name, req.BaseURL, req.APIKey, req.ModelType, req.Owner = j.Name, j.BaseURL, j.APIKey, j.ModelType, j.Owner
		} else {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			req.Name = r.FormValue("name")
			req.BaseURL = r.FormValue("base_url")
			req.APIKey = r.FormValue("api_key")
			req.ModelType = r.FormValue("model_type")
			req.Owner = r.FormValue("owner")
		}
		if req.ModelType == "" {
			req.ModelType = "llm"
		}
		endpoint := &db.ModelEndpoint{
			ID:        uuid.New().String(),
			Name:      req.Name,
			BaseURL:   req.BaseURL,
			ModelType: req.ModelType,
			IsEnabled: true,
		}
		if req.APIKey != "" {
			endpoint.APIKey = sql.NullString{String: req.APIKey, Valid: true}
		}
		if req.Owner != "" {
			endpoint.Owner = sql.NullString{String: req.Owner, Valid: true}
		}
		if err := s.db.CreateModelEndpoint(endpoint); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		skipProbe := r.FormValue("skip_probe") == "true"
		var models []string
		online := false
		if !skipProbe {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			models, _ = s.probeEndpointModelsSync(ctx, endpoint)
			if len(models) > 0 {
				online = true
				data, _ := json.Marshal(models)
				endpoint.CachedModels = sql.NullString{String: string(data), Valid: true}
				s.db.UpdateModelEndpoint(endpoint)
			}
		} else {
			online = true
			go s.probeEndpointModels(endpoint)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      endpoint.ID,
			"name":    endpoint.Name,
			"base_url": endpoint.BaseURL,
			"online":  online,
			"models":  models,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEndpointByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	isAdmin := s.auth.IsAdmin(user)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/model-endpoints/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	endpoint, err := s.db.GetModelEndpoint(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		return
	}

	if action == "models" && r.Method == http.MethodGet {
		var allModels []string
		if endpoint.CachedModels.Valid && endpoint.CachedModels.String != "" {
			json.Unmarshal([]byte(endpoint.CachedModels.String), &allModels)
		}
		var hidden []string
		if endpoint.HiddenModels.Valid && endpoint.HiddenModels.String != "" {
			json.Unmarshal([]byte(endpoint.HiddenModels.String), &hidden)
		}
		hiddenSet := make(map[string]bool, len(hidden))
		for _, h := range hidden {
			hiddenSet[h] = true
		}
		type modelEntry struct {
			ID       string `json:"id"`
			Display  string `json:"display"`
			IsHidden bool   `json:"is_hidden"`
		}
		result := make([]modelEntry, 0, len(allModels))
		for _, m := range allModels {
			parts := strings.Split(m, "/")
			display := parts[len(parts)-1]
			result = append(result, modelEntry{ID: m, Display: display, IsHidden: hiddenSet[m]})
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if action == "probe" {
		if r.Method == http.MethodGet {
			// SSE probe — streams per-model results
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")
			flusher, _ := w.(http.Flusher)

			sendEvent := func(data string) {
				fmt.Fprintf(w, "data: %s\n\n", data)
				if flusher != nil {
					flusher.Flush()
				}
			}

			models, err := s.probeEndpointModelsSync(r.Context(), endpoint)
			if err != nil {
				b, _ := json.Marshal(map[string]any{"type": "probe_start", "endpoint": endpoint.BaseURL, "model_count": 0, "error": err.Error()})
				sendEvent(string(b))
				b, _ = json.Marshal(map[string]any{"type": "probe_done", "ok": 0, "hidden": 0, "error": err.Error()})
				sendEvent(string(b))
				return
			}

			// Save models to DB
			if len(models) > 0 {
				data, _ := json.Marshal(models)
				endpoint.CachedModels = sql.NullString{String: string(data), Valid: true}
				s.db.UpdateModelEndpoint(endpoint)
			}

			b, _ := json.Marshal(map[string]any{"type": "probe_start", "endpoint": endpoint.BaseURL, "model_count": len(models)})
			sendEvent(string(b))

			ok := 0
			for _, m := range models {
				start := time.Now()
				b, _ := json.Marshal(map[string]any{
					"type":       "probe_result",
					"model":      m,
					"status":     "ok",
					"latency_ms": time.Since(start).Milliseconds(),
				})
				sendEvent(string(b))
				ok++
			}
			b, _ = json.Marshal(map[string]any{"type": "probe_done", "ok": ok, "hidden": 0})
			sendEvent(string(b))
			return
		}
		if r.Method == http.MethodPost {
			models, err := s.probeEndpointModelsSync(r.Context(), endpoint)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"models": models})
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		if endpoint.APIKey.Valid {
			endpoint.APIKey = sql.NullString{String: "stored", Valid: true}
		}
		writeJSON(w, http.StatusOK, endpoint)
	case http.MethodPut, http.MethodPatch:
		if !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin only"})
			return
		}
		var name, baseURL, apiKey, modelType *string
		var isEnabled, supportsTools *bool
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			var req struct {
				Name          *string `json:"name"`
				BaseURL       *string `json:"base_url"`
				APIKey        *string `json:"api_key"`
				IsEnabled     *bool   `json:"is_enabled"`
				ModelType     *string `json:"model_type"`
				SupportsTools *bool   `json:"supports_tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			name, baseURL, apiKey, isEnabled, modelType, supportsTools = req.Name, req.BaseURL, req.APIKey, req.IsEnabled, req.ModelType, req.SupportsTools
		} else if ct != "" {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			if v := r.FormValue("name"); v != "" {
				name = &v
			}
			if v := r.FormValue("base_url"); v != "" {
				baseURL = &v
			}
			if v := r.FormValue("api_key"); v != "" {
				apiKey = &v
			}
			if v := r.FormValue("is_enabled"); v != "" {
				b := v == "true" || v == "1"
				isEnabled = &b
			}
			if v := r.FormValue("model_type"); v != "" {
				modelType = &v
			}
			if v := r.FormValue("supports_tools"); v != "" {
				b := v == "true" || v == "1"
				supportsTools = &b
			}
		} else {
			// Empty body PATCH = toggle is_enabled
			toggled := !endpoint.IsEnabled
			isEnabled = &toggled
		}
		if name != nil {
			endpoint.Name = *name
		}
		if baseURL != nil {
			endpoint.BaseURL = *baseURL
		}
		if apiKey != nil && *apiKey != "stored" {
			endpoint.APIKey = sql.NullString{String: *apiKey, Valid: *apiKey != ""}
		}
		if isEnabled != nil {
			endpoint.IsEnabled = *isEnabled
		}
		if modelType != nil {
			endpoint.ModelType = *modelType
		}
		if supportsTools != nil {
			endpoint.SupportsTools = sql.NullBool{Bool: *supportsTools, Valid: true}
		}
		if err := s.db.UpdateModelEndpoint(endpoint); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case http.MethodDelete:
		if !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin only"})
			return
		}
		if err := s.db.DeleteModelEndpoint(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) probeEndpointModels(endpoint *db.ModelEndpoint) {
	ctx := context.Background()
	models, err := s.probeEndpointModelsSync(ctx, endpoint)
	if err != nil || len(models) == 0 {
		return
	}
	data, _ := json.Marshal(models)
	endpoint.CachedModels = sql.NullString{String: string(data), Valid: true}
	if err := s.db.UpdateModelEndpoint(endpoint); err != nil {
		log.Printf("db: UpdateModelEndpoint: %v", err)
	}
}

func (s *Server) probeEndpointModelsSync(ctx context.Context, endpoint *db.ModelEndpoint) ([]string, error) {
	client := llm.New()
	headers := map[string]string{}
	if endpoint.APIKey.Valid && endpoint.APIKey.String != "" && endpoint.APIKey.String != "stored" {
		headers["Authorization"] = "Bearer " + endpoint.APIKey.String
	}
	return client.DiscoverModels(ctx, endpoint.BaseURL, headers)
}
