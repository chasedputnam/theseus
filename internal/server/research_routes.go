package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/research"
	"github.com/chaseputnam/theseus/internal/search"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

// ResearchRecord is a persisted summary of a completed research job.
type ResearchRecord struct {
	ID        string    `json:"id"`
	Question  string    `json:"question"`
	Owner     string    `json:"owner"`
	Archived  bool      `json:"archived"`
	Report    string    `json:"report"`
	Sources   []search.Result `json:"sources"`
	Rounds    int       `json:"rounds"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) researchStoreFile(user string) string {
	return s.cfg.DataDir + "/research_" + sanitizeFilename(user) + ".json"
}

func (s *Server) loadResearchRecords(user string) []*ResearchRecord {
	var records []*ResearchRecord
	storage.ReadJSON(s.researchStoreFile(user), &records)
	if records == nil {
		records = []*ResearchRecord{}
	}
	return records
}

func (s *Server) saveResearchRecords(user string, records []*ResearchRecord) {
	storage.WriteJSON(s.researchStoreFile(user), records)
}

func (s *Server) persistResearchRecord(user, jobID, question string, result *research.Result) {
	if result == nil {
		return
	}
	rec := &ResearchRecord{
		ID:        jobID,
		Question:  question,
		Owner:     user,
		Report:    result.Report,
		Sources:   result.Sources,
		Rounds:    result.Rounds,
		CreatedAt: time.Now().UTC(),
	}
	records := s.loadResearchRecords(user)
	records = append(records, rec)
	s.saveResearchRecords(user, records)
}

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
	researchWG     sync.WaitGroup
)

func (s *Server) registerResearchRoutes() {
	s.mux.HandleFunc("/api/research/start", s.withAuth(s.handleResearchStart))
	s.mux.HandleFunc("/api/research/library", s.withAuth(s.handleResearchLibrary))
	s.mux.HandleFunc("/api/research/active", s.withAuth(s.handleResearchActive))
	s.mux.HandleFunc("/api/research/detail/", s.withAuth(s.handleResearchDetail))
	s.mux.HandleFunc("/api/research/report/", s.withAuth(s.handleResearchReport))
	s.mux.HandleFunc("/api/research/spinoff/", s.withAuth(s.handleResearchSpinoff))
	s.mux.HandleFunc("/api/research/status/", s.withAuth(s.handleResearchStatus))
	s.mux.HandleFunc("/api/research/result/", s.withAuth(s.handleResearchResult))
	s.mux.HandleFunc("/api/research/result-peek/", s.withAuth(s.handleResearchResultPeek))
	s.mux.HandleFunc("/api/research/cancel/", s.withAuth(s.handleResearchCancel))
	s.mux.HandleFunc("/api/research/", s.withAuth(s.handleResearchByID))
}

// evictOldResearchJobs removes completed jobs beyond 500 total from the in-memory map.
func evictOldResearchJobs() {
	researchJobsMu.Lock()
	defer researchJobsMu.Unlock()
	const maxJobs = 500
	if len(researchJobs) <= maxJobs {
		return
	}
	// Collect done job IDs and remove oldest until under limit.
	var doneIDs []string
	for id, j := range researchJobs {
		j.mu.Lock()
		if j.done {
			doneIDs = append(doneIDs, id)
		}
		j.mu.Unlock()
	}
	for i := 0; i < len(doneIDs) && len(researchJobs) > maxJobs; i++ {
		delete(researchJobs, doneIDs[i])
	}
}

func (s *Server) handleResearchStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	jobID := strings.TrimPrefix(r.URL.Path, "/api/research/status/")
	jobID = strings.TrimSuffix(jobID, "/")

	researchJobsMu.RLock()
	job, inMemory := researchJobs[jobID]
	researchJobsMu.RUnlock()

	if inMemory {
		job.mu.Lock()
		done := job.done
		var errStr interface{}
		if job.err != nil {
			errStr = job.err.Error()
		}
		job.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "done": done, "error": errStr})
		return
	}
	for _, rec := range s.loadResearchRecords(user) {
		if rec.ID == jobID {
			writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "done": true, "error": nil})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
}

func (s *Server) handleResearchResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	jobID := strings.TrimPrefix(r.URL.Path, "/api/research/result/")
	jobID = strings.TrimSuffix(jobID, "/")

	researchJobsMu.RLock()
	job, inMemory := researchJobs[jobID]
	researchJobsMu.RUnlock()

	if inMemory {
		job.mu.Lock()
		done := job.done
		result := job.result
		jobErr := job.err
		job.mu.Unlock()
		if !done {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "in_progress"})
			return
		}
		if jobErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": jobErr.Error()})
			return
		}
		if result != nil {
			writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "report": result.Report, "sources": result.Sources, "rounds": result.Rounds})
			return
		}
	}
	for _, rec := range s.loadResearchRecords(user) {
		if rec.ID == jobID {
			writeJSON(w, http.StatusOK, map[string]any{"job_id": rec.ID, "report": rec.Report, "sources": rec.Sources, "rounds": rec.Rounds})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
}

func (s *Server) handleResearchResultPeek(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	jobID := strings.TrimPrefix(r.URL.Path, "/api/research/result-peek/")
	jobID = strings.TrimSuffix(jobID, "/")

	researchJobsMu.RLock()
	job, inMemory := researchJobs[jobID]
	researchJobsMu.RUnlock()

	if inMemory {
		job.mu.Lock()
		done := job.done
		result := job.result
		job.mu.Unlock()
		if !done || result == nil {
			writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "done": false, "partial": ""})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "done": true, "partial": result.Report})
		return
	}
	for _, rec := range s.loadResearchRecords(user) {
		if rec.ID == jobID {
			writeJSON(w, http.StatusOK, map[string]any{"job_id": rec.ID, "done": true, "partial": rec.Report})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
}

func (s *Server) handleResearchCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/api/research/cancel/")
	jobID = strings.TrimSuffix(jobID, "/")

	researchJobsMu.RLock()
	job, inMemory := researchJobs[jobID]
	researchJobsMu.RUnlock()

	if !inMemory {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	job.cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
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
	go evictOldResearchJobs()

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

	s.persistResearchRecord(user, jobID, req.Question, result)

	d, _ := json.Marshal(map[string]any{
		"job_id":  jobID,
		"report":  result.Report,
		"sources": result.Sources,
		"rounds":  result.Rounds,
	})
	sendEvent("done", string(d))
}

func (s *Server) handleResearchLibrary(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	archived := r.URL.Query().Get("archived") == "true"
	records := s.loadResearchRecords(user)
	filtered := make([]*ResearchRecord, 0)
	for _, rec := range records {
		if rec.Archived == archived {
			filtered = append(filtered, rec)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"research": filtered, "jobs": filtered, "total": len(filtered)})
}

func (s *Server) handleResearchActive(w http.ResponseWriter, r *http.Request) {
	researchJobsMu.RLock()
	active := make([]map[string]any, 0)
	for id, job := range researchJobs {
		job.mu.Lock()
		if !job.done {
			active = append(active, map[string]any{"job_id": id})
		}
		job.mu.Unlock()
	}
	researchJobsMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"active": active, "count": len(active)})
}

func (s *Server) handleResearchDetail(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/research/detail/")
	for _, rec := range s.loadResearchRecords(user) {
		if rec.ID == id {
			writeJSON(w, http.StatusOK, rec)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) handleResearchReport(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/research/report/")
	for _, rec := range s.loadResearchRecords(user) {
		if rec.ID == id {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(rec.Report))
			return
		}
	}
	// Fall back to in-memory job
	researchJobsMu.RLock()
	job, ok := researchJobs[id]
	researchJobsMu.RUnlock()
	if ok {
		job.mu.Lock()
		result := job.result
		job.mu.Unlock()
		if result != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(result.Report))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) handleResearchSpinoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/research/spinoff/")
	msgs, err := s.db.ListMessages(sessionID)
	if err != nil || len(msgs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session not found or empty"})
		return
	}
	lastMsg := msgs[len(msgs)-1]
	jobID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	job := &researchJob{ID: jobID, cancel: cancel}
	researchJobsMu.Lock()
	researchJobs[jobID] = job
	researchJobsMu.Unlock()
	go evictOldResearchJobs()
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
		Question:    lastMsg.Content,
		EndpointURL: settings.GetString("research_endpoint_id"),
		Model:       settings.GetString("research_model"),
		MaxRounds:   5,
		MaxTokens:   settings.GetInt("research_max_tokens"),
		Owner:       user,
		JobID:       jobID,
	}
	researchWG.Add(1)
	go func() {
		defer researchWG.Done()
		result, err := engine.Run(ctx, researchReq, nil)
		job.mu.Lock()
		job.result = result
		job.err = err
		job.done = true
		job.mu.Unlock()
		if err == nil {
			s.persistResearchRecord(user, jobID, lastMsg.Content, result)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]string{"job_id": jobID})
}

func (s *Server) handleResearchByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/research/"), "/")
	jobID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	researchJobsMu.RLock()
	job, inMemory := researchJobs[jobID]
	researchJobsMu.RUnlock()

	// For DELETE, also handle persisted records
	if r.Method == http.MethodDelete && action == "" {
		records := s.loadResearchRecords(user)
		newRecords := make([]*ResearchRecord, 0, len(records))
		found := false
		for _, rec := range records {
			if rec.ID == jobID {
				found = true
			} else {
				newRecords = append(newRecords, rec)
			}
		}
		if found {
			s.saveResearchRecords(user, newRecords)
		}
		if inMemory {
			job.cancel()
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Fall back to persistent store for GET when not in memory
	if !inMemory {
		for _, rec := range s.loadResearchRecords(user) {
			if rec.ID == jobID {
				writeJSON(w, http.StatusOK, map[string]any{
					"job_id": rec.ID,
					"done":   true,
					"error":  nil,
				})
				return
			}
		}
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
