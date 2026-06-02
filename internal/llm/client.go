package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	deadHostCooldown  = 20 * time.Second
	hostFailThreshold = 2
	streamTimeout     = 300 * time.Second
	defaultTimeout    = 30 * time.Second
)

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []ContentPart
}

// ContentPart for vision messages.
type ContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *ImageURLObj `json:"image_url,omitempty"`
}

type ImageURLObj struct {
	URL string `json:"url"`
}

// ToolSchema is an OpenAI-style function tool definition.
type ToolSchema struct {
	Type     string         `json:"type"`
	Function ToolFunctionDef `json:"function"`
}

type ToolFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall is a function call emitted by the LLM.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// StreamChunk is one SSE delta from the LLM.
type StreamChunk struct {
	Delta        string
	FinishReason string
	ToolCalls    []ToolCall
	Error        error
}

// StreamRequest configures a streaming LLM call.
type StreamRequest struct {
	URL         string
	Model       string
	Messages    []Message
	Headers     map[string]string
	Temperature float64
	MaxTokens   int
	Tools       []ToolSchema
	Stream      bool
}

// CallRequest configures a non-streaming LLM call.
type CallRequest struct {
	URL         string
	Model       string
	Messages    []Message
	Headers     map[string]string
	Temperature float64
	MaxTokens   int
}

// Client is a reusable LLM HTTP client with dead-host tracking.
type Client struct {
	http       *http.Client
	mu         sync.Mutex
	deadHosts  map[string]time.Time
	hostFails  map[string]int
}

// New creates a new LLM Client.
func New() *Client {
	return &Client{
		http: &http.Client{
			Timeout: streamTimeout,
		},
		deadHosts: make(map[string]time.Time),
		hostFails: make(map[string]int),
	}
}

func (c *Client) hostKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Scheme + "://" + u.Host
}

func (c *Client) isHostDead(rawURL string) bool {
	key := c.hostKey(rawURL)
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.deadHosts[key]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(c.deadHosts, key)
		delete(c.hostFails, key)
		return false
	}
	return true
}

func (c *Client) recordFailure(rawURL string) {
	key := c.hostKey(rawURL)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hostFails[key]++
	if c.hostFails[key] >= hostFailThreshold {
		c.deadHosts[key] = time.Now().Add(deadHostCooldown)
		log.Printf("llm: host %s marked dead for %v", key, deadHostCooldown)
	}
}

func (c *Client) recordSuccess(rawURL string) {
	key := c.hostKey(rawURL)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.deadHosts, key)
	delete(c.hostFails, key)
}

// Stream sends a streaming chat completion request and returns a channel of chunks.
func (c *Client) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	if c.isHostDead(req.URL) {
		return nil, fmt.Errorf("LLM endpoint unreachable (cooldown): %s", c.hostKey(req.URL))
	}

	endpoint := NormalizeBaseURL(req.URL) + "/v1/chat/completions"

	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.recordFailure(req.URL)
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.recordFailure(req.URL)
		return nil, fmt.Errorf("LLM error %d: %s", resp.StatusCode, string(body))
	}
	c.recordSuccess(req.URL)

	ch := make(chan StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				return
			}
			var event struct {
				Choices []struct {
					Delta struct {
						Content   string     `json:"content"`
						ToolCalls []ToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}
			for _, choice := range event.Choices {
				chunk := StreamChunk{
					Delta:        choice.Delta.Content,
					FinishReason: choice.FinishReason,
					ToolCalls:    choice.Delta.ToolCalls,
				}
				select {
				case ch <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			select {
			case ch <- StreamChunk{Error: err}:
			default:
			}
		}
	}()
	return ch, nil
}

// Call sends a non-streaming chat completion and returns the full response text.
func (c *Client) Call(ctx context.Context, req CallRequest) (string, error) {
	if c.isHostDead(req.URL) {
		return "", fmt.Errorf("LLM endpoint unreachable (cooldown): %s", c.hostKey(req.URL))
	}

	endpoint := NormalizeBaseURL(req.URL) + "/v1/chat/completions"

	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   false,
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.recordFailure(req.URL)
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		c.recordFailure(req.URL)
		return "", fmt.Errorf("LLM error %d: %s", resp.StatusCode, string(b))
	}
	c.recordSuccess(req.URL)

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}

// NormalizeBaseURL strips trailing slashes and a trailing /v1 so callers
// can safely append /v1/... without doubling the prefix.
func NormalizeBaseURL(u string) string {
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/v1/chat/completions")
	u = strings.TrimSuffix(u, "/v1")
	return u
}


func (c *Client) DiscoverModels(ctx context.Context, baseURL string, headers map[string]string) ([]string, error) {
	base := NormalizeBaseURL(baseURL)
	endpoint := base + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	httpCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req = req.WithContext(httpCtx)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("auth required (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}
