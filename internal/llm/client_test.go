package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamSSEParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	c := New()
	ch, err := c.Stream(context.Background(), StreamRequest{
		URL:   srv.URL,
		Model: "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		sb.WriteString(chunk.Delta)
	}
	if got := sb.String(); got != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", got)
	}
}

func TestDeadHostCooldown(t *testing.T) {
	c := New()
	// Simulate 2 failures
	badURL := "http://127.0.0.1:19999"
	for i := 0; i < hostFailThreshold; i++ {
		c.recordFailure(badURL)
	}
	if !c.isHostDead(badURL) {
		t.Fatal("host should be dead after threshold failures")
	}
	// Success resets
	c.recordSuccess(badURL)
	if c.isHostDead(badURL) {
		t.Fatal("host should be alive after success")
	}
}

func TestDeadHostCooldownExpiry(t *testing.T) {
	c := New()
	key := c.hostKey("http://example.com")
	c.mu.Lock()
	c.deadHosts[key] = time.Now().Add(-1 * time.Second) // already expired
	c.mu.Unlock()
	if c.isHostDead("http://example.com") {
		t.Fatal("expired cooldown should not be dead")
	}
}

func TestCallNonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer srv.Close()

	c := New()
	got, err := c.Call(context.Background(), CallRequest{
		URL:   srv.URL,
		Model: "test",
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != "pong" {
		t.Errorf("expected pong, got %q", got)
	}
}

func TestDiscoverModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"data":[{"id":"gpt-4"},{"id":"gpt-3.5-turbo"}]}`)
	}))
	defer srv.Close()

	c := New()
	models, err := c.DiscoverModels(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}
