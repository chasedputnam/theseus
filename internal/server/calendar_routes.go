package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/calendar"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/google/uuid"
)

func (s *Server) registerCalendarRoutes() {
	s.mux.HandleFunc("/api/calendars", s.withAuth(s.handleCalendars))
	s.mux.HandleFunc("/api/calendars/", s.withAuth(s.handleCalendarByID))
	s.mux.HandleFunc("/api/calendar/events", s.withAuth(s.handleCalendarEvents))
	s.mux.HandleFunc("/api/calendar/sync", s.withAuth(s.handleCalendarSync))
	s.mux.HandleFunc("/api/calendar/import", s.withAuth(s.handleCalendarImport))
}

func (s *Server) handleCalendars(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		cals, err := s.db.ListCalendars(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if cals == nil {
			cals = []*db.CalendarCal{}
		}
		writeJSON(w, http.StatusOK, cals)
	case http.MethodPost:
		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Color == "" {
			req.Color = "#4A90D9"
		}
		cal := &db.CalendarCal{
			ID:        uuid.New().String(),
			Owner:     sql.NullString{String: user, Valid: user != ""},
			Name:      req.Name,
			Color:     req.Color,
			Source:    "local",
			IsVisible: true,
		}
		if err := s.db.CreateCalendar(cal); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cal)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCalendarByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/calendars/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	cal, err := s.db.GetCalendar(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "calendar not found"})
		return
	}
	if cal.Owner.Valid && cal.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if action == "export" {
		from := time.Now().AddDate(-1, 0, 0)
		to := time.Now().AddDate(1, 0, 0)
		events, _ := s.db.ListEvents(id, from, to)
		var remoteEvents []calendar.RemoteEvent
		for _, e := range events {
			remoteEvents = append(remoteEvents, calendar.RemoteEvent{
				UID:     e.UID,
				Summary: e.Title,
				Start:   e.StartTime,
				End:     e.EndTime,
				AllDay:  e.AllDay,
				RRule:   e.RRule.String,
			})
		}
		ics := calendar.ExportICS(cal.Name, remoteEvents)
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+cal.Name+`.ics"`)
		w.Write([]byte(ics))
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, cal)
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Name      *string `json:"name"`
			Color     *string `json:"color"`
			IsVisible *bool   `json:"is_visible"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name != nil {
			cal.Name = *req.Name
		}
		if req.Color != nil {
			cal.Color = *req.Color
		}
		if req.IsVisible != nil {
			cal.IsVisible = *req.IsVisible
		}
		if err := s.db.UpdateCalendar(cal); err != nil {
			log.Printf("db: UpdateCalendar: %v", err)
		}
		writeJSON(w, http.StatusOK, cal)
	case http.MethodDelete:
		if err := s.db.DeleteCalendar(id); err != nil {
			log.Printf("db: DeleteCalendar: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCalendarEvents(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")
		from := time.Now().AddDate(0, -1, 0)
		to := time.Now().AddDate(0, 3, 0)
		if fromStr != "" {
			if t, err := time.Parse("2006-01-02", fromStr); err == nil {
				from = t
			}
		}
		if toStr != "" {
			if t, err := time.Parse("2006-01-02", toStr); err == nil {
				to = t
			}
		}
		events, err := s.db.ListEventsByOwner(user, from, to)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if events == nil {
			events = []*db.CalendarEvent{}
		}
		writeJSON(w, http.StatusOK, events)
	case http.MethodPost:
		var req struct {
			CalendarID  string `json:"calendar_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Location    string `json:"location"`
			StartTime   string `json:"start_time"`
			EndTime     string `json:"end_time"`
			AllDay      bool   `json:"all_day"`
			RRule       string `json:"rrule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		start, _ := time.Parse(time.RFC3339, req.StartTime)
		end, _ := time.Parse(time.RFC3339, req.EndTime)
		event := &db.CalendarEvent{
			ID:          uuid.New().String(),
			UID:         uuid.New().String(),
			CalendarID:  req.CalendarID,
			Title:       req.Title,
			Description: sql.NullString{String: req.Description, Valid: req.Description != ""},
			Location:    sql.NullString{String: req.Location, Valid: req.Location != ""},
			StartTime:   start,
			EndTime:     end,
			AllDay:      req.AllDay,
			RRule:       sql.NullString{String: req.RRule, Valid: req.RRule != ""},
			IsUTC:       true,
		}
		if err := s.db.CreateEvent(event); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, event)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCalendarSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	cfg := calendar.SyncConfig{
		URL:      settings.GetString("caldav_url"),
		Username: settings.GetString("caldav_username"),
		Password: settings.GetString("caldav_password"),
		Owner:    user,
	}
	syncer := calendar.NewSyncer(s.db)
	if err := syncer.Sync(r.Context(), cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCalendarImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	r.ParseMultipartForm(10 << 20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file"})
		return
	}
	defer file.Close()

	var buf []byte
	buf = make([]byte, 10<<20)
	n, _ := file.Read(buf)
	buf = buf[:n]

	events, err := calendar.ParseICS(buf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ICS: " + err.Error()})
		return
	}

	calID := r.FormValue("calendar_id")
	if calID == "" {
		// Create a default calendar
		cal := &db.CalendarCal{
			ID:        uuid.New().String(),
			Owner:     sql.NullString{String: user, Valid: user != ""},
			Name:      "Imported",
			Color:     "#4A90D9",
			Source:    "local",
			IsVisible: true,
		}
		if err := s.db.CreateCalendar(cal); err != nil {
			log.Printf("db: CreateCalendar: %v", err)
		}
		calID = cal.ID
	}

	imported := 0
	for _, ev := range events {
		dbEvent := &db.CalendarEvent{
			ID:          uuid.New().String(),
			UID:         ev.UID,
			CalendarID:  calID,
			Title:       ev.Summary,
			Description: sql.NullString{String: ev.Description, Valid: ev.Description != ""},
			Location:    sql.NullString{String: ev.Location, Valid: ev.Location != ""},
			StartTime:   ev.Start,
			EndTime:     ev.End,
			AllDay:      ev.AllDay,
			RRule:       sql.NullString{String: ev.RRule, Valid: ev.RRule != ""},
			IsUTC:       true,
		}
		if err := s.db.UpsertEvent(dbEvent); err == nil {
			imported++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}
