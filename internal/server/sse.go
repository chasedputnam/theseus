package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEWriter writes Server-Sent Events to an http.ResponseWriter.
// It is safe for concurrent use.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

// NewSSEWriter creates an SSEWriter and sets the required SSE headers.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	f, _ := w.(http.Flusher)
	return &SSEWriter{w: w, flusher: f}
}

// Send writes a named SSE event with a data payload.
func (s *SSEWriter) Send(event, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// SendJSON marshals v to JSON and sends it as the named event.
func (s *SSEWriter) SendJSON(event string, v any) {
	d, _ := json.Marshal(v)
	s.Send(event, string(d))
}

// SendDelta sends a content delta event.
func (s *SSEWriter) SendDelta(content string) {
	s.SendJSON("delta", map[string]string{"content": content})
}

// SendDone sends the terminal done event.
func (s *SSEWriter) SendDone() {
	s.Send("done", `{"status":"done"}`)
}

// SendError sends an error event.
func (s *SSEWriter) SendError(err error) {
	s.SendJSON("error", map[string]string{"error": err.Error()})
}
