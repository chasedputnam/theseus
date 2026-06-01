package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chaseputnam/theseus/internal/llm"
)

type mockDispatcher struct {
	calls []ToolBlock
}

func (m *mockDispatcher) Execute(ctx context.Context, block ToolBlock, owner string, privs map[string]any) (string, error) {
	m.calls = append(m.calls, block)
	return "mock result for " + block.ToolType, nil
}

func TestAgentLoopNoTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Just a plain answer"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	rr := httptest.NewRecorder()
	sse := NewSSEWriter(rr)
	dispatcher := &mockDispatcher{}
	client := llm.New()

	err := Run(context.Background(), Request{
		EndpointURL: srv.URL,
		Model:       "test",
		Messages:    []llm.Message{{Role: "user", Content: "hello"}},
	}, client, dispatcher, sse)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(dispatcher.calls))
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Just a plain answer") {
		t.Errorf("expected response in SSE output, got: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("expected done event")
	}
}

func TestAgentLoopWithTool(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		callCount++
		if callCount == 1 {
			// First call: return a tool block
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Let me run that:\n` + "```" + `bash\necho hello\n` + "```" + `"},"finish_reason":"stop"}]}`)
		} else {
			// Second call: plain answer after tool result
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Done!"},"finish_reason":"stop"}]}`)
		}
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	rr := httptest.NewRecorder()
	sse := NewSSEWriter(rr)
	dispatcher := &mockDispatcher{}
	client := llm.New()

	err := Run(context.Background(), Request{
		EndpointURL: srv.URL,
		Model:       "test",
		Messages:    []llm.Message{{Role: "user", Content: "run echo hello"}},
	}, client, dispatcher, sse)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(dispatcher.calls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(dispatcher.calls))
	}
	if dispatcher.calls[0].ToolType != "bash" {
		t.Errorf("expected bash tool, got %s", dispatcher.calls[0].ToolType)
	}
}
