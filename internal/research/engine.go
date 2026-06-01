package research

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/chaseputnam/theseus/internal/search"
)

// Engine runs the iterative Think→Search→Extract→Synthesize research loop.
type Engine struct {
	llm    *llm.Client
	search *search.Client
}

// New creates a research Engine.
func New(llmClient *llm.Client, searchClient *search.Client) *Engine {
	return &Engine{llm: llmClient, search: searchClient}
}

// Request carries parameters for one research run.
type Request struct {
	Question    string
	EndpointURL string
	Model       string
	Headers     map[string]string
	MaxRounds   int
	MaxTokens   int
	Owner       string
	JobID       string
}

// ProgressEvent is an SSE event emitted during research.
type ProgressEvent struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Round   int    `json:"round,omitempty"`
	Query   string `json:"query,omitempty"`
	Source  string `json:"source,omitempty"`
}

// Result is the final research output.
type Result struct {
	Report  string          `json:"report"`
	Sources []search.Result `json:"sources"`
	Rounds  int             `json:"rounds"`
}

// Run executes the research loop, emitting progress events to the channel.
func (e *Engine) Run(ctx context.Context, req Request, progress chan<- ProgressEvent) (*Result, error) {
	if req.MaxRounds <= 0 {
		req.MaxRounds = 5
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 16384
	}

	progress <- ProgressEvent{Type: "start", Message: "Planning research..."}

	// Phase 1: Generate research plan
	plan, err := e.generatePlan(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("plan generation: %w", err)
	}
	progress <- ProgressEvent{Type: "plan", Message: "Research plan ready"}

	var allSources []search.Result
	report := ""
	seenURLs := make(map[string]bool)

	for round := 1; round <= req.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		progress <- ProgressEvent{Type: "round_start", Round: round, Message: fmt.Sprintf("Round %d: generating queries...", round)}

		// Phase 2: Generate search queries
		queries, err := e.generateQueries(ctx, req, plan, report, round)
		if err != nil || len(queries) == 0 {
			break
		}

		// Phase 3: Search and extract
		var roundSources []search.Result
		for _, query := range queries {
			progress <- ProgressEvent{Type: "searching", Query: query, Round: round}
			results, err := e.search.Search(ctx, query, 3)
			if err != nil {
				continue
			}
			for _, r := range results {
				if seenURLs[r.URL] {
					continue
				}
				seenURLs[r.URL] = true
				// Fetch content
				content, err := e.search.FetchContent(ctx, r.URL)
				if err == nil && content != "" {
					r.Snippet = truncate(content, 2000)
				}
				roundSources = append(roundSources, r)
				progress <- ProgressEvent{Type: "source", Source: r.URL, Round: round}
			}
		}
		allSources = append(allSources, roundSources...)

		// Phase 4: Synthesize
		progress <- ProgressEvent{Type: "synthesizing", Round: round, Message: "Synthesizing findings..."}
		newReport, err := e.synthesize(ctx, req, plan, report, roundSources, round)
		if err != nil {
			break
		}
		report = newReport

		// Check if we have enough
		if e.isComplete(ctx, req, report, round) {
			break
		}
	}

	progress <- ProgressEvent{Type: "generating_report", Message: "Generating final report..."}

	// Generate HTML report
	htmlReport := GenerateHTMLReport(req.Question, report, allSources, time.Now())

	progress <- ProgressEvent{Type: "done", Message: "Research complete"}

	return &Result{
		Report:  htmlReport,
		Sources: allSources,
		Rounds:  req.MaxRounds,
	}, nil
}

const researchPlanPrompt = `You are a research strategist. Analyze this question and create a research plan.

**Question:** %s

Return a JSON object with:
- "sub_questions": Array of 3-6 specific sub-questions to investigate
- "key_topics": Array of key topics/angles to cover
- "success_criteria": One sentence describing what a complete answer looks like

Return ONLY valid JSON, no other text.`

func (e *Engine) generatePlan(ctx context.Context, req Request) (map[string]any, error) {
	prompt := fmt.Sprintf(researchPlanPrompt, req.Question)
	resp, err := e.llm.Call(ctx, llm.CallRequest{
		URL:       req.EndpointURL,
		Model:     req.Model,
		Headers:   req.Headers,
		MaxTokens: 1024,
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return map[string]any{"sub_questions": []string{req.Question}}, nil
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(extractJSON(resp)), &plan); err != nil {
		return map[string]any{"sub_questions": []string{req.Question}}, nil
	}
	return plan, nil
}

const queryGenPrompt = `You are a research assistant planning web searches.

**Original question:** %s
**Research plan:** %s
**What we know so far:** %s
**Round:** %d

Generate 2-3 specific search queries to find missing information.
Return ONLY a JSON array of query strings, e.g. ["query 1", "query 2"]`

func (e *Engine) generateQueries(ctx context.Context, req Request, plan map[string]any, report string, round int) ([]string, error) {
	planJSON, _ := json.Marshal(plan)
	summary := truncate(report, 2000)
	prompt := fmt.Sprintf(queryGenPrompt, req.Question, string(planJSON), summary, round)

	resp, err := e.llm.Call(ctx, llm.CallRequest{
		URL:       req.EndpointURL,
		Model:     req.Model,
		Headers:   req.Headers,
		MaxTokens: 512,
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return []string{req.Question}, nil
	}
	var queries []string
	if err := json.Unmarshal([]byte(extractJSON(resp)), &queries); err != nil {
		// Fallback: use the question itself
		return []string{req.Question}, nil
	}
	return queries, nil
}

const synthesizePrompt = `You are a research analyst synthesizing findings.

**Question:** %s
**Current report:** %s

**New sources found:**
%s

Update and expand the report with the new information. Write in clear, well-structured markdown.
Focus on answering the original question comprehensively. Cite sources with [Source: URL] notation.`

func (e *Engine) synthesize(ctx context.Context, req Request, plan map[string]any, currentReport string, sources []search.Result, round int) (string, error) {
	var sourcesText strings.Builder
	for _, s := range sources {
		sourcesText.WriteString(fmt.Sprintf("**%s** (%s)\n%s\n\n", s.Title, s.URL, truncate(s.Snippet, 500)))
	}

	prompt := fmt.Sprintf(synthesizePrompt, req.Question, truncate(currentReport, 3000), sourcesText.String())
	resp, err := e.llm.Call(ctx, llm.CallRequest{
		URL:       req.EndpointURL,
		Model:     req.Model,
		Headers:   req.Headers,
		MaxTokens: req.MaxTokens,
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return currentReport, err
	}
	return stripThinking(resp), nil
}

func (e *Engine) isComplete(ctx context.Context, req Request, report string, round int) bool {
	// Simple heuristic: if report is substantial and we've done at least 2 rounds
	return round >= 2 && len(report) > 2000
}

// extractJSON finds the first JSON object or array in a string.
func extractJSON(s string) string {
	// Try to find JSON array
	if idx := strings.Index(s, "["); idx != -1 {
		end := strings.LastIndex(s, "]")
		if end > idx {
			return s[idx : end+1]
		}
	}
	// Try JSON object
	if idx := strings.Index(s, "{"); idx != -1 {
		end := strings.LastIndex(s, "}")
		if end > idx {
			return s[idx : end+1]
		}
	}
	return s
}

// stripThinking removes <think>...</think> blocks from LLM output.
func stripThinking(s string) string {
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
