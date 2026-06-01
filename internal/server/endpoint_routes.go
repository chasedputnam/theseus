package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"

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
		for _, e := range endpoints {
			if e.APIKey.Valid && e.APIKey.String != "" {
				e.APIKey = sql.NullString{String: "stored", Valid: true}
			}
		}
		writeJSON(w, http.StatusOK, endpoints)
	case http.MethodPost:
		if !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin only"})
			return
		}
		var req struct {
			Name      string `json:"name"`
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			ModelType string `json:"model_type"`
			Owner     string `json:"owner"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
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
		go s.probeEndpointModels(endpoint)
		writeJSON(w, http.StatusOK, endpoint)
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

	if action == "probe" && r.Method == http.MethodPost {
		models, err := s.probeEndpointModelsSync(r.Context(), endpoint)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
		return
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
		if req.Name != nil {
			endpoint.Name = *req.Name
		}
		if req.BaseURL != nil {
			endpoint.BaseURL = *req.BaseURL
		}
		if req.APIKey != nil && *req.APIKey != "stored" {
			endpoint.APIKey = sql.NullString{String: *req.APIKey, Valid: *req.APIKey != ""}
		}
		if req.IsEnabled != nil {
			endpoint.IsEnabled = *req.IsEnabled
		}
		if req.ModelType != nil {
			endpoint.ModelType = *req.ModelType
		}
		if req.SupportsTools != nil {
			endpoint.SupportsTools = sql.NullBool{Bool: *req.SupportsTools, Valid: true}
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
