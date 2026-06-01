package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/research"
	"github.com/chaseputnam/theseus/internal/search"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/google/uuid"
)

type researchJob struct {
	ID     string
	cancel context.CancelFunc
	result *research.Result
	err    error
	done   bool
	mu     sync.Mutex
}

var (
	researchJobs   = make(map[string]*researchJob)
	researchJobsMu sync.RWMutex
)

func (s *Server) registerResearchRoutes() {
	s.mux.HandleFunc("/api/research/start", s.withAuth(s.handleResearchStart))
	s.mux.HandleFunc("/api/research/", s.withAuth(s.handleResearchByID))
}

func (s *Server) handleResearchStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	if !s.auth.HasPrivilege(user, "can_use_research") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Research not permitted"})
		return
	}

	var req struct {
		Question    string `json:"question"`
		SessionID   string `json:"session_id"`
		EndpointURL string `json:"endpoint_url"`
		Model       string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// Resolve endpoint/model from settings if not provided
	endpointURL := req.EndpointURL
	model := req.Model
	if endpointURL == "" {
		endpointURL = settings.GetString("research_endpoint_id")
	}
	if model == "" {
		model = settings.GetString("research_model")
	}

	jobID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())

	job := &researchJob{ID: jobID, cancel: cancel}
	researchJobsMu.Lock()
	researchJobs[jobID] = job
	researchJobsMu.Unlock()

	// Build search client from settings
	searchClient := search.BuildFromSettings(
		settings.GetString("search_provider"),
		settings.GetString("search_url"),
		[]string{"duckduckgo"},
		settings.GetString("brave_api_key"),
		settings.GetString("google_pse_key"),
		settings.GetString("google_pse_cx"),
		settings.GetString("tavily_api_key"),
		settings.GetString("serper_api_key"),
	)

	engine := research.New(llm.New(), searchClient)
	researchReq := research.Request{
		Question:    req.Question,
		EndpointURL: endpointURL,
		Model:       model,
		MaxRounds:   5,
		MaxTokens:   settings.GetInt("research_max_tokens"),
		Owner:       user,
		JobID:       jobID,
	}

	// Stream SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	sendEvent := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	progress := make(chan research.ProgressEvent, 32)
	go func() {
		result, err := engine.Run(ctx, researchReq, progress)
		job.mu.Lock()
		job.result = result
		job.err = err
		job.done = true
		job.mu.Unlock()
		close(progress)
	}()

	// Forward progress events
	for event := range progress {
		d, _ := json.Marshal(event)
		sendEvent("progress", string(d))
	}

	job.mu.Lock()
	result := job.result
	jobErr := job.err
	job.mu.Unlock()

	if jobErr != nil {
		d, _ := json.Marshal(map[string]string{"error": jobErr.Error()})
		sendEvent("error", string(d))
		return
	}

	d, _ := json.Marshal(map[string]any{
		"job_id":  jobID,
		"report":  result.Report,
		"sources": result.Sources,
		"rounds":  result.Rounds,
	})
	sendEvent("done", string(d))
}

func (s *Server) handleResearchByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/research/"), "/")
	jobID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	researchJobsMu.RLock()
	job, ok := researchJobs[jobID]
	researchJobsMu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	switch action {
	case "cancel":
		if r.Method == http.MethodDelete {
			job.cancel()
			writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
		}
	case "report":
		job.mu.Lock()
		result := job.result
		done := job.done
		job.mu.Unlock()
		if !done || result == nil {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "in_progress"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(result.Report))
	default:
		job.mu.Lock()
		done := job.done
		jobErr := job.err
		job.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"job_id": jobID,
			"done":   done,
			"error":  jobErr,
		})
	}
}
