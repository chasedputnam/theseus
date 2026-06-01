package calendar

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

type basicAuthClient struct {
	username string
	password string
	inner    http.Client
}

func (b *basicAuthClient) Do(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(b.username, b.password)
	return b.inner.Do(req)
}

var _ webdav.HTTPClient = (*basicAuthClient)(nil)

func findCalendarsViaWebDAV(ctx context.Context, baseURL, username, password string) ([]RemoteCalendar, error) {
	httpClient := &basicAuthClient{username: username, password: password}
	client, err := caldav.NewClient(httpClient, baseURL)
	if err != nil {
		return nil, err
	}
	principal, err := client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	homeSet, err := client.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return nil, err
	}
	cals, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return nil, err
	}
	var result []RemoteCalendar
	for _, cal := range cals {
		result = append(result, RemoteCalendar{
			Path:  cal.Path,
			Name:  cal.Name,
			Color: "#4A90D9",
		})
	}
	return result, nil
}

func getEventsViaWebDAV(ctx context.Context, baseURL, username, password, calPath string, from, to time.Time) ([]RemoteEvent, error) {
	httpClient := &basicAuthClient{username: username, password: password}
	client, err := caldav.NewClient(httpClient, baseURL)
	if err != nil {
		return nil, err
	}
	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:     "VCALENDAR",
			AllComps: true,
			AllProps: true,
		},
		CompFilter: caldav.CompFilter{
			Name: "VCALENDAR",
			Comps: []caldav.CompFilter{{
				Name:  "VEVENT",
				Start: from,
				End:   to,
			}},
		},
	}
	objects, err := client.QueryCalendar(ctx, calPath, query)
	if err != nil {
		return nil, err
	}
	var events []RemoteEvent
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		for _, ev := range obj.Data.Events() {
			remote := parseICalEvent(ev)
			if remote != nil {
				events = append(events, *remote)
			}
		}
	}
	return events, nil
}

func parseICalEvent(ev ical.Event) *RemoteEvent {
	uid := ev.Props.Get(ical.PropUID)
	summary := ev.Props.Get(ical.PropSummary)
	if uid == nil || summary == nil {
		return nil
	}
	remote := &RemoteEvent{
		UID:     uid.Value,
		Summary: summary.Value,
	}
	if desc := ev.Props.Get(ical.PropDescription); desc != nil {
		remote.Description = desc.Value
	}
	if loc := ev.Props.Get(ical.PropLocation); loc != nil {
		remote.Location = loc.Value
	}
	if rrule := ev.Props.Get(ical.PropRecurrenceRule); rrule != nil {
		remote.RRule = rrule.Value
	}
	start, err := ev.DateTimeStart(time.UTC)
	if err == nil {
		remote.Start = start
	}
	end, err := ev.DateTimeEnd(time.UTC)
	if err == nil {
		remote.End = end
	}
	if remote.End.IsZero() {
		remote.End = remote.Start.Add(time.Hour)
	}
	return remote
}

// ExportICS generates an ICS file from a list of events.
func ExportICS(calName string, events []RemoteEvent) string {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Theseus//EN\r\n")
	sb.WriteString("X-WR-CALNAME:" + escapeICS(calName) + "\r\n")
	for _, ev := range events {
		sb.WriteString("BEGIN:VEVENT\r\n")
		sb.WriteString("UID:" + ev.UID + "\r\n")
		sb.WriteString("SUMMARY:" + escapeICS(ev.Summary) + "\r\n")
		if ev.AllDay {
			sb.WriteString("DTSTART;VALUE=DATE:" + ev.Start.Format("20060102") + "\r\n")
			sb.WriteString("DTEND;VALUE=DATE:" + ev.End.Format("20060102") + "\r\n")
		} else {
			sb.WriteString("DTSTART:" + ev.Start.UTC().Format("20060102T150405Z") + "\r\n")
			sb.WriteString("DTEND:" + ev.End.UTC().Format("20060102T150405Z") + "\r\n")
		}
		if ev.Description != "" {
			sb.WriteString("DESCRIPTION:" + escapeICS(ev.Description) + "\r\n")
		}
		if ev.Location != "" {
			sb.WriteString("LOCATION:" + escapeICS(ev.Location) + "\r\n")
		}
		if ev.RRule != "" {
			sb.WriteString("RRULE:" + ev.RRule + "\r\n")
		}
		sb.WriteString("END:VEVENT\r\n")
	}
	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

func escapeICS(s string) string {
	s = strings.ReplaceAll(s, ``, `\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// ParseICS parses an ICS file and returns events.
func ParseICS(data []byte) ([]RemoteEvent, error) {
	dec := ical.NewDecoder(bytes.NewReader(data))
	cal, err := dec.Decode()
	if err != nil {
		return nil, err
	}
	var events []RemoteEvent
	for _, ev := range cal.Events() {
		remote := parseICalEvent(ev)
		if remote != nil {
			events = append(events, *remote)
		}
	}
	return events, nil
}
