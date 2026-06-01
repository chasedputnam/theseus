package db

import (
	"database/sql"
	"fmt"
	"time"
)

func (db *DB) CreateCalendar(c *CalendarCal) error {
	_, err := db.Exec(`INSERT INTO calendar_cals (id,owner,name,color,source,remote_url,is_visible,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Owner, c.Name, c.Color, c.Source, c.RemoteURL, boolInt(c.IsVisible), now(), now(),
	)
	return err
}

func (db *DB) GetCalendar(id string) (*CalendarCal, error) {
	row := db.QueryRow(`SELECT id,owner,name,color,source,remote_url,is_visible,created_at,updated_at FROM calendar_cals WHERE id=?`, id)
	return scanCalendar(row)
}

func (db *DB) ListCalendars(owner string) ([]*CalendarCal, error) {
	q := `SELECT id,owner,name,color,source,remote_url,is_visible,created_at,updated_at FROM calendar_cals`
	args := []any{}
	if owner != "" {
		q += " WHERE owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY name"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cals []*CalendarCal
	for rows.Next() {
		c, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		cals = append(cals, c)
	}
	return cals, rows.Err()
}

func (db *DB) UpdateCalendar(c *CalendarCal) error {
	_, err := db.Exec(`UPDATE calendar_cals SET name=?,color=?,is_visible=?,updated_at=? WHERE id=?`,
		c.Name, c.Color, boolInt(c.IsVisible), now(), c.ID)
	return err
}

func (db *DB) DeleteCalendar(id string) error {
	_, err := db.Exec(`DELETE FROM calendar_cals WHERE id=?`, id)
	return err
}

func (db *DB) CreateEvent(e *CalendarEvent) error {
	_, err := db.Exec(`INSERT INTO calendar_events
		(id,uid,calendar_id,title,description,location,start_time,end_time,all_day,rrule,is_utc,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.UID, e.CalendarID, e.Title, e.Description, e.Location,
		e.StartTime.UTC().Format(time.RFC3339), e.EndTime.UTC().Format(time.RFC3339),
		boolInt(e.AllDay), e.RRule, boolInt(e.IsUTC), now(), now(),
	)
	return err
}

func (db *DB) GetEvent(uid string) (*CalendarEvent, error) {
	row := db.QueryRow(`SELECT id,uid,calendar_id,title,description,location,start_time,end_time,
		all_day,rrule,is_utc,created_at,updated_at FROM calendar_events WHERE uid=?`, uid)
	return scanEvent(row)
}

func (db *DB) ListEvents(calendarID string, from, to time.Time) ([]*CalendarEvent, error) {
	rows, err := db.Query(`SELECT id,uid,calendar_id,title,description,location,start_time,end_time,
		all_day,rrule,is_utc,created_at,updated_at FROM calendar_events
		WHERE calendar_id=? AND start_time >= ? AND end_time <= ? ORDER BY start_time`,
		calendarID, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*CalendarEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (db *DB) ListEventsByOwner(owner string, from, to time.Time) ([]*CalendarEvent, error) {
	rows, err := db.Query(`SELECT e.id,e.uid,e.calendar_id,e.title,e.description,e.location,
		e.start_time,e.end_time,e.all_day,e.rrule,e.is_utc,e.created_at,e.updated_at
		FROM calendar_events e JOIN calendar_cals c ON e.calendar_id=c.id
		WHERE c.owner=? AND e.start_time >= ? AND e.end_time <= ? ORDER BY e.start_time`,
		owner, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*CalendarEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (db *DB) UpsertEvent(e *CalendarEvent) error {
	_, err := db.Exec(`INSERT INTO calendar_events
		(id,uid,calendar_id,title,description,location,start_time,end_time,all_day,rrule,is_utc,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(uid) DO UPDATE SET
		title=excluded.title,description=excluded.description,location=excluded.location,
		start_time=excluded.start_time,end_time=excluded.end_time,all_day=excluded.all_day,
		rrule=excluded.rrule,is_utc=excluded.is_utc,updated_at=excluded.updated_at`,
		e.ID, e.UID, e.CalendarID, e.Title, e.Description, e.Location,
		e.StartTime.UTC().Format(time.RFC3339), e.EndTime.UTC().Format(time.RFC3339),
		boolInt(e.AllDay), e.RRule, boolInt(e.IsUTC), now(), now(),
	)
	return err
}

func (db *DB) UpdateEvent(e *CalendarEvent) error {
	_, err := db.Exec(`UPDATE calendar_events SET
		title=?,description=?,location=?,start_time=?,end_time=?,all_day=?,rrule=?,updated_at=?
		WHERE uid=?`,
		e.Title, e.Description, e.Location,
		e.StartTime.UTC().Format(time.RFC3339), e.EndTime.UTC().Format(time.RFC3339),
		boolInt(e.AllDay), e.RRule, now(), e.UID,
	)
	return err
}

func (db *DB) DeleteEvent(uid string) error {
	_, err := db.Exec(`DELETE FROM calendar_events WHERE uid=?`, uid)
	return err
}

func (db *DB) DeleteEventsByCalendarNotIn(calendarID string, uids []string) error {
	if len(uids) == 0 {
		_, err := db.Exec(`DELETE FROM calendar_events WHERE calendar_id=?`, calendarID)
		return err
	}
	// Build NOT IN clause
	placeholders := make([]string, len(uids))
	args := make([]any, len(uids)+1)
	args[0] = calendarID
	for i, uid := range uids {
		placeholders[i] = "?"
		args[i+1] = uid
	}
	q := fmt.Sprintf(`DELETE FROM calendar_events WHERE calendar_id=? AND uid NOT IN (%s)`,
		joinStrings(placeholders, ","))
	_, err := db.Exec(q, args...)
	return err
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func scanCalendar(row scanner) (*CalendarCal, error) {
	c := &CalendarCal{}
	var isVisible int
	var createdAt, updatedAt string
	err := row.Scan(&c.ID, &c.Owner, &c.Name, &c.Color, &c.Source, &c.RemoteURL, &isVisible, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("calendar not found")
		}
		return nil, err
	}
	c.IsVisible = isVisible != 0
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return c, nil
}

func scanEvent(row scanner) (*CalendarEvent, error) {
	e := &CalendarEvent{}
	var allDay, isUTC int
	var startTime, endTime, createdAt, updatedAt string
	err := row.Scan(&e.ID, &e.UID, &e.CalendarID, &e.Title, &e.Description, &e.Location,
		&startTime, &endTime, &allDay, &e.RRule, &isUTC, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("event not found")
		}
		return nil, err
	}
	e.AllDay = allDay != 0
	e.IsUTC = isUTC != 0
	e.StartTime, _ = time.Parse(time.RFC3339, startTime)
	e.EndTime, _ = time.Parse(time.RFC3339, endTime)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return e, nil
}
