package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/llm"
)

const (
	MaxAgentRounds = 20
	ToolTimeout    = 60 * time.Second
	MaxOutputChars = 10_000
)

// Request carries parameters for one agent loop invocation.
type Request struct {
	SessionID   string
	Messages    []llm.Message
	Owner       string
	EndpointURL string
	Model       string
	Headers     map[string]string
	Privileges  map[string]any
	ActiveDocID string
	MCPManager  interface{} // *mcp.Manager — kept as interface to avoid circular import
}

// Dispatcher executes a single tool block and returns the result string.
type Dispatcher interface {
	Execute(ctx context.Context, block ToolBlock, owner string, privileges map[string]any) (string, error)
}

// SSEWriter writes SSE events to an http.ResponseWriter.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	f, _ := w.(http.Flusher)
	return &SSEWriter{w: w, flusher: f}
}

func (s *SSEWriter) Send(event, data string) {
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *SSEWriter) SendDelta(content string) {
	d, _ := json.Marshal(map[string]string{"delta": content})
	s.Send("delta", string(d))
}

func (s *SSEWriter) SendToolStart(toolType string) {
	d, _ := json.Marshal(map[string]string{"tool": toolType, "status": "running"})
	s.Send("tool_start", string(d))
}

func (s *SSEWriter) SendToolResult(toolType, result string) {
	d, _ := json.Marshal(map[string]string{"tool": toolType, "result": result})
	s.Send("tool_result", string(d))
}

func (s *SSEWriter) SendDone() {
	s.Send("done", `{"status":"done"}`)
	fmt.Fprintf(s.w, "data: [DONE]\n\n")
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *SSEWriter) SendError(err error) {
	d, _ := json.Marshal(map[string]string{"error": err.Error(), "text": err.Error()})
	s.Send("error", string(d))
}

// agentPreamble is injected as the system prompt prefix.
const agentPreamble = `You are an AI assistant with tool access. You can run shell commands, execute Python, search the web, read/write files, create and edit documents, generate images, manage memories, and more. To use a tool, write a fenced code block with the tool name as the language tag. The block executes automatically and you see the output.

## Rules
- Only use tools when needed. Don't search for things you already know.
- These exact tags execute automatically. For showing code examples, use ` + "```" + `shell, ` + "```" + `sh, ` + "```" + `py, etc. instead.
- Multiple tool blocks per response OK. 60s timeout per tool, 10K char output limit.
- Code/content >15 lines → ` + "```" + `create_document (NOT in chat). Short snippets OK in chat.
- BIAS TOWARD ACTION on edit requests. Just do it with your best interpretation.
- AFTER A TOOL SUCCEEDS, reply in ONE short sentence confirming what was done.
- AFTER A TOOL FAILS, DO NOT GO SILENT. Retry with a fix or tell the user what failed.`

// Run executes the agent loop, streaming output to sse.
func Run(ctx context.Context, req Request, llmClient *llm.Client, dispatcher Dispatcher, sse *SSEWriter) error {
	messages := make([]llm.Message, 0, len(req.Messages)+1)

	// Inject agent preamble as system message if not already present
	hasSystem := false
	for _, m := range req.Messages {
		if m.Role == "system" {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		messages = append(messages, llm.Message{Role: "system", Content: agentPreamble})
	}
	messages = append(messages, req.Messages...)

	for round := 0; round < MaxAgentRounds; round++ {
		// Stream LLM response
		streamReq := llm.StreamRequest{
			URL:      req.EndpointURL,
			Model:    req.Model,
			Messages: messages,
			Headers:  req.Headers,
		}

		ch, err := llmClient.Stream(ctx, streamReq)
		if err != nil {
			sse.SendError(err)
			return err
		}

		var responseBuilder strings.Builder
		var toolCalls []llm.ToolCall

		for chunk := range ch {
			if chunk.Error != nil {
				sse.SendError(chunk.Error)
				return chunk.Error
			}
			if chunk.Delta != "" {
				responseBuilder.WriteString(chunk.Delta)
				sse.SendDelta(chunk.Delta)
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = append(toolCalls, chunk.ToolCalls...)
			}
		}

		fullResponse := responseBuilder.String()

		// Parse fenced tool blocks from response text
		toolBlocks := ParseToolBlocks(fullResponse)

		// Also handle OpenAI function-calling tool_calls
		for _, tc := range toolCalls {
			toolBlocks = append(toolBlocks, ToolBlock{
				ToolType: tc.Function.Name,
				Content:  tc.Function.Arguments,
			})
		}

		// If no tool blocks, we're done
		if len(toolBlocks) == 0 {
			// Append assistant message and return
			messages = append(messages, llm.Message{Role: "assistant", Content: fullResponse})
			sse.SendDone()
			return nil
		}

		// Append assistant message
		messages = append(messages, llm.Message{Role: "assistant", Content: fullResponse})

		// Execute each tool block
		for _, block := range toolBlocks {
			sse.SendToolStart(block.ToolType)

			toolCtx, cancel := context.WithTimeout(ctx, ToolTimeout)
			result, err := dispatcher.Execute(toolCtx, block, req.Owner, req.Privileges)
			cancel()

			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			if len(result) > MaxOutputChars {
				result = result[:MaxOutputChars] + fmt.Sprintf("\n... (truncated, %d chars total)", len(result))
			}

			sse.SendToolResult(block.ToolType, result)

			// Append tool result as user message (tool feedback)
			toolMsg := fmt.Sprintf("Tool `%s` result:\n%s", block.ToolType, result)
			messages = append(messages, llm.Message{Role: "user", Content: toolMsg})
		}
	}

	// Reached max rounds
	sse.Send("warning", `{"message":"Reached maximum agent rounds"}`)
	sse.SendDone()
	return nil
}
