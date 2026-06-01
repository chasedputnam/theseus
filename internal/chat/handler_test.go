package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/google/uuid"
)

func setupTest(t *testing.T) (*db.DB, *auth.Manager) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	mgr := auth.New(
		filepath.Join(dir, "auth.json"),
		filepath.Join(dir, "sessions.json"),
	)
	mgr.Setup("admin", "pass")
	return database, mgr
}

func TestToolIntentDetection(t *testing.T) {
	cases := []struct {
		text   string
		expect bool
	}{
		{"remind me to call mom", true},
		{"add to calendar tomorrow", true},
		{"what is the weather?", false},
		{"generate an image of a cat", true},
		{"send an email to bob", true},
		{"hello how are you", false},
	}
	for _, tc := range cases {
		got := hasToolIntent(tc.text)
		if got != tc.expect {
			t.Errorf("hasToolIntent(%q) = %v, want %v", tc.text, got, tc.expect)
		}
	}
}

func TestChatStreamingSSE(t *testing.T) {
	// Mock LLM server
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" there"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer llmSrv.Close()

	database, mgr := setupTest(t)
	defer database.Close()

	// Create a session
	sess := &db.Session{
		ID:          uuid.New().String(),
		Name:        "test",
		EndpointURL: llmSrv.URL,
		Model:       "test-model",
		Headers:     "{}",
	}
	database.CreateSession(sess)

	llmClient := llm.New()
	h := New(database, llmClient, mgr, nil)

	body := fmt.Sprintf(`{"session_id":%q,"message":"hi","mode":"chat"}`, sess.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.UserKey, "admin"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	resp := rr.Body.String()
	if !strings.Contains(resp, "Hello") {
		t.Errorf("expected 'Hello' in SSE response, got: %s", resp)
	}
	if !strings.Contains(resp, "event: done") {
		t.Errorf("expected 'done' event in SSE response")
	}

	// Verify assistant message was persisted
	msgs, err := database.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	var hasAssistant bool
	for _, m := range msgs {
		if m.Role == "assistant" {
			hasAssistant = true
			if !strings.Contains(m.Content, "Hello") {
				t.Errorf("assistant message should contain 'Hello', got %q", m.Content)
			}
		}
	}
	if !hasAssistant {
		t.Error("expected assistant message to be persisted")
	}
	_ = json.Marshal
	_ = time.Now
}
