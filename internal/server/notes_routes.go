package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/google/uuid"
)

func (s *Server) registerNotesRoutes() {
	s.mux.HandleFunc("/api/notes", s.withAuth(s.handleNotes))
	s.mux.HandleFunc("/api/notes/fire-reminder", s.withAuth(s.handleNotesFireReminder))
	s.mux.HandleFunc("/api/notes/reorder", s.withAuth(s.handleNotesReorder))
	s.mux.HandleFunc("/api/notes/", s.withAuth(s.handleNoteByID))
	s.mux.HandleFunc("/api/tasks", s.withAuth(s.handleTasks))
	s.mux.HandleFunc("/api/tasks/meta/actions", s.withAuth(s.handleTasksMeta))
	s.mux.HandleFunc("/api/tasks/meta/events", s.withAuth(s.handleTasksMeta))
	s.mux.HandleFunc("/api/tasks/meta/output-targets", s.withAuth(s.handleTasksMeta))
	s.mux.HandleFunc("/api/tasks/notifications", s.withAuth(s.handleTasksNotifications))
	s.mux.HandleFunc("/api/tasks/onboarding", s.withAuth(s.handleTasksOnboarding))
	s.mux.HandleFunc("/api/tasks/parse", s.withAuth(s.handleTasksParse))
	s.mux.HandleFunc("/api/tasks/runs/recent", s.withAuth(s.handleTasksRunsRecent))
	s.mux.HandleFunc("/api/tasks/", s.withAuth(s.handleTaskByID))
}

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		archived := r.URL.Query().Get("archived") == "true"
		notes, err := s.db.ListNotes(user, archived)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if notes == nil {
			notes = []*db.Note{}
		}
		writeJSON(w, http.StatusOK, notes)
	case http.MethodPost:
		var req struct {
			Title    string          `json:"title"`
			Content  string          `json:"content"`
			Items    json.RawMessage `json:"items"`
			NoteType string          `json:"note_type"`
			Color    string          `json:"color"`
			Label    string          `json:"label"`
			Pinned   bool            `json:"pinned"`
			DueDate  string          `json:"due_date"`
			Repeat   string          `json:"repeat"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.NoteType == "" {
			req.NoteType = "note"
		}
		if req.Repeat == "" {
			req.Repeat = "none"
		}
		note := &db.Note{
			ID:       uuid.New().String(),
			Owner:    sql.NullString{String: user, Valid: user != ""},
			Title:    req.Title,
			Content:  sql.NullString{String: req.Content, Valid: req.Content != ""},
			NoteType: req.NoteType,
			Color:    sql.NullString{String: req.Color, Valid: req.Color != ""},
			Label:    sql.NullString{String: req.Label, Valid: req.Label != ""},
			Pinned:   req.Pinned,
			DueDate:  sql.NullString{String: req.DueDate, Valid: req.DueDate != ""},
			Source:   "user",
			Repeat:   req.Repeat,
		}
		if len(req.Items) > 0 {
			note.Items = sql.NullString{String: string(req.Items), Valid: true}
		}
		if err := s.db.CreateNote(note); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, note)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNoteByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/notes/")

	note, err := s.db.GetNote(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
		return
	}
	if note.Owner.Valid && note.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, note)
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Title    *string          `json:"title"`
			Content  *string          `json:"content"`
			Items    *json.RawMessage `json:"items"`
			Color    *string          `json:"color"`
			Label    *string          `json:"label"`
			Pinned   *bool            `json:"pinned"`
			Archived *bool            `json:"archived"`
			DueDate  *string          `json:"due_date"`
			Repeat   *string          `json:"repeat"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Title != nil {
			note.Title = *req.Title
		}
		if req.Content != nil {
			note.Content = sql.NullString{String: *req.Content, Valid: true}
		}
		if req.Items != nil {
			note.Items = sql.NullString{String: string(*req.Items), Valid: true}
		}
		if req.Color != nil {
			note.Color = sql.NullString{String: *req.Color, Valid: *req.Color != ""}
		}
		if req.Label != nil {
			note.Label = sql.NullString{String: *req.Label, Valid: *req.Label != ""}
		}
		if req.Pinned != nil {
			note.Pinned = *req.Pinned
		}
		if req.Archived != nil {
			note.Archived = *req.Archived
		}
		if req.DueDate != nil {
			note.DueDate = sql.NullString{String: *req.DueDate, Valid: *req.DueDate != ""}
		}
		if req.Repeat != nil {
			note.Repeat = *req.Repeat
		}
		if err := s.db.UpdateNote(note); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, note)
	case http.MethodDelete:
		if err := s.db.DeleteNote(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		tasks, err := s.db.ListScheduledTasks(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tasks == nil {
			tasks = []*db.ScheduledTask{}
		}
		writeJSON(w, http.StatusOK, tasks)
	case http.MethodPost:
		var req struct {
			Name          string `json:"name"`
			Prompt        string `json:"prompt"`
			TaskType      string `json:"task_type"`
			Schedule      string `json:"schedule"`
			ScheduledTime string `json:"scheduled_time"`
			Model         string `json:"model"`
			EndpointURL   string `json:"endpoint_url"`
			CronExpr      string `json:"cron_expression"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.TaskType == "" {
			req.TaskType = "llm"
		}
		task := &db.ScheduledTask{
			ID:                   uuid.New().String(),
			Owner:                sql.NullString{String: user, Valid: user != ""},
			Name:                 req.Name,
			Prompt:               sql.NullString{String: req.Prompt, Valid: req.Prompt != ""},
			TaskType:             req.TaskType,
			Schedule:             sql.NullString{String: req.Schedule, Valid: req.Schedule != ""},
			ScheduledTime:        sql.NullString{String: req.ScheduledTime, Valid: req.ScheduledTime != ""},
			Model:                sql.NullString{String: req.Model, Valid: req.Model != ""},
			EndpointURL:          sql.NullString{String: req.EndpointURL, Valid: req.EndpointURL != ""},
			CronExpression:       sql.NullString{String: req.CronExpr, Valid: req.CronExpr != ""},
			Status:               "active",
			TriggerType:          "schedule",
			OutputTarget:         "session",
			EmailResults:         true,
			NotificationsEnabled: true,
		}
		// Compute next_run
		task.NextRun = computeNextRun(task)
		if err := s.db.CreateScheduledTask(task); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, task)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	task, err := s.db.GetScheduledTask(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Owner.Valid && task.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if action == "runs" {
		limit := 20
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		runs, _ := s.db.ListTaskRuns(id, limit)
		if runs == nil {
			runs = []*db.TaskRun{}
		}
		writeJSON(w, http.StatusOK, runs)
		return
	}

	if r.Method == http.MethodPost && (action == "run" || action == "pause" || action == "resume" || action == "revert") {
		switch action {
		case "run":
			run := &db.TaskRun{
				ID:        uuid.New().String(),
				TaskID:    id,
				StartedAt: time.Now().UTC(),
				Status:    "running",
			}
			if err := s.db.CreateTaskRun(run); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			task.Status = "running"
			task.LastRun = sql.NullTime{Time: time.Now().UTC(), Valid: true}
			s.db.UpdateScheduledTask(task)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run_id": run.ID})
		case "pause":
			task.Status = "paused"
			s.db.UpdateScheduledTask(task)
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case "resume":
			task.Status = "active"
			s.db.UpdateScheduledTask(task)
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case "revert":
			task.Status = "active"
			s.db.UpdateScheduledTask(task)
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, task)
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Name   *string `json:"name"`
			Status *string `json:"status"`
			Prompt *string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Name != nil {
			task.Name = *req.Name
		}
		if req.Status != nil {
			task.Status = *req.Status
		}
		if req.Prompt != nil {
			task.Prompt = sql.NullString{String: *req.Prompt, Valid: true}
		}
		if err := s.db.UpdateScheduledTask(task); err != nil {
			log.Printf("db: UpdateScheduledTask: %v", err)
		}
		writeJSON(w, http.StatusOK, task)
	case http.MethodDelete:
		if err := s.db.DeleteScheduledTask(id); err != nil {
			log.Printf("db: DeleteScheduledTask: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNotesReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	for i, id := range req.IDs {
		s.db.UpdateNoteSortOrder(id, i)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleNotesFireReminder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		NoteID string `json:"note_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "note_id": req.NoteID})
}

func (s *Server) handleTasksMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/tasks/meta/")
	switch suffix {
	case "actions":
		writeJSON(w, http.StatusOK, map[string]any{"actions": []string{
			"send_message", "run_shell", "create_note", "create_document",
			"send_email", "webhook", "research", "summarize",
		}})
	case "events":
		writeJSON(w, http.StatusOK, map[string]any{"events": []string{
			"schedule", "on_message", "on_document_created", "on_email_received",
			"on_session_ended", "manual",
		}})
	case "output-targets":
		writeJSON(w, http.StatusOK, map[string]any{"output_targets": []string{
			"session", "note", "document", "email", "webhook", "none",
		}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown meta key"})
	}
}

func (s *Server) handleTasksNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	tasks, err := s.db.ListScheduledTasks(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var notifications []map[string]any
	for _, t := range tasks {
		if t.Status == "failed" || t.Status == "completed" {
			runs, _ := s.db.ListTaskRuns(t.ID, 1)
			var lastRun *db.TaskRun
			if len(runs) > 0 {
				lastRun = runs[0]
			}
			n := map[string]any{
				"task_id":   t.ID,
				"task_name": t.Name,
				"status":    t.Status,
			}
			if lastRun != nil {
				n["run_id"] = lastRun.ID
				n["finished_at"] = lastRun.FinishedAt
				if lastRun.Error.Valid {
					n["error"] = lastRun.Error.String
				}
			}
			notifications = append(notifications, n)
		}
	}
	if notifications == nil {
		notifications = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": notifications})
}

func (s *Server) handleTasksOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": []map[string]any{
		{"name": "Daily Summary", "prompt": "Summarize my notes from today", "schedule": "daily", "task_type": "llm"},
		{"name": "Weekly Report", "prompt": "Create a weekly report from my sessions", "schedule": "weekly", "task_type": "llm"},
		{"name": "Email Digest", "prompt": "Summarize unread emails", "schedule": "daily", "task_type": "llm", "output_target": "email"},
	}})
}

func (s *Server) handleTasksParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	endpointURL := settings.GetString("default_endpoint_url")
	model := settings.GetString("default_model")
	prompt := `Parse the following natural language task description into a JSON object with fields: name (string), prompt (string), schedule (one of: once, daily, weekly, monthly, or empty), task_type (string), trigger_type (string). Return only valid JSON.

Text: ` + req.Text
	var result strings.Builder
	lc := llm.New()
	ch, err := lc.Stream(r.Context(), llm.StreamRequest{
		URL:   endpointURL,
		Model: model,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err == nil {
		for chunk := range ch {
			if chunk.Error == nil {
				result.WriteString(chunk.Delta)
			}
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.String()), &parsed); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"raw": result.String()})
		return
	}
	writeJSON(w, http.StatusOK, parsed)
}

func (s *Server) handleTasksRunsRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	tasks, err := s.db.ListScheduledTasks(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var allRuns []*db.TaskRun
	for _, t := range tasks {
		runs, _ := s.db.ListTaskRuns(t.ID, 100)
		allRuns = append(allRuns, runs...)
	}
	if allRuns == nil {
		allRuns = []*db.TaskRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": allRuns})
}

// computeNextRun calculates the next run time for a task.
func computeNextRun(t *db.ScheduledTask) sql.NullTime {
	now := time.Now().UTC()
	schedule := t.Schedule.String
	switch schedule {
	case "once":
		if t.ScheduledDate.Valid {
			return sql.NullTime{Time: t.ScheduledDate.Time, Valid: true}
		}
	case "daily":
		next := now.Add(24 * time.Hour)
		return sql.NullTime{Time: next, Valid: true}
	case "weekly":
		next := now.Add(7 * 24 * time.Hour)
		return sql.NullTime{Time: next, Valid: true}
	case "monthly":
		next := now.AddDate(0, 1, 0)
		return sql.NullTime{Time: next, Valid: true}
	}
	return sql.NullTime{Valid: false}
}
