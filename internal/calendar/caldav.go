package calendar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/chaseputnam/theseus/internal/db"
	"github.com/google/uuid"
)

// SyncConfig holds CalDAV connection parameters.
type SyncConfig struct {
	URL      string
	Username string
	Password string
	Owner    string
}

// Syncer pulls CalDAV calendars and events into the local SQLite store.
type Syncer struct {
	db *db.DB
}

// NewSyncer creates a CalDAV Syncer.
func NewSyncer(database *db.DB) *Syncer {
	return &Syncer{db: database}
}

// Sync pulls all calendars and events from the CalDAV server.
func (s *Syncer) Sync(ctx context.Context, cfg SyncConfig) error {
	if cfg.URL == "" {
		return fmt.Errorf("caldav URL not configured")
	}
	calendars, err := findCalendarsViaWebDAV(ctx, cfg.URL, cfg.Username, cfg.Password)
	if err != nil {
		return fmt.Errorf("caldav find calendars: %w", err)
	}

	for _, remoteCal := range calendars {
		calID := stableCalID(remoteCal.Path)
		existing, err := s.db.GetCalendar(calID)
		if err != nil {
			cal := &db.CalendarCal{
				ID:        calID,
				Owner:     sql.NullString{String: cfg.Owner, Valid: cfg.Owner != ""},
				Name:      remoteCal.Name,
				Color:     remoteCal.Color,
				Source:    "caldav",
				RemoteURL: sql.NullString{String: remoteCal.Path, Valid: true},
				IsVisible: true,
			}
			if err := s.db.CreateCalendar(cal); err != nil {
				log.Printf("caldav: create calendar %s: %v", remoteCal.Name, err)
				continue
			}
		} else {
			existing.Name = remoteCal.Name
			s.db.UpdateCalendar(existing)
		}

		from := time.Now().AddDate(0, 0, -90)
		to := time.Now().AddDate(1, 0, 0)
		events, err := getEventsViaWebDAV(ctx, cfg.URL, cfg.Username, cfg.Password, remoteCal.Path, from, to)
		if err != nil {
			log.Printf("caldav: get events for %s: %v", remoteCal.Name, err)
			continue
		}

		var seenUIDs []string
		for _, ev := range events {
			uid := ev.UID
			if uid == "" {
				uid = uuid.New().String()
			}
			seenUIDs = append(seenUIDs, uid)
			dbEvent := &db.CalendarEvent{
				ID:          uuid.New().String(),
				UID:         uid,
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
			if err := s.db.UpsertEvent(dbEvent); err != nil {
				log.Printf("caldav: upsert event %s: %v", uid, err)
			}
		}
		s.db.DeleteEventsByCalendarNotIn(calID, seenUIDs)
	}
	return nil
}

func stableCalID(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8])
}

// RemoteCalendar is a calendar from the CalDAV server.
type RemoteCalendar struct {
	Path  string
	Name  string
	Color string
}

// RemoteEvent is an event from the CalDAV server.
type RemoteEvent struct {
	UID         string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool
	RRule       string
}
