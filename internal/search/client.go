package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Result is a single search result.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Provider is a search backend.
type Provider interface {
	Search(ctx context.Context, query string, n int) ([]Result, error)
	Name() string
}

// Client performs web searches with provider fallback and TTL caching.
type Client struct {
	providers []Provider
	mu        sync.RWMutex
	cache     map[string]cacheEntry
	cacheTTL  time.Duration
	httpClient *http.Client
}

type cacheEntry struct {
	results []Result
	expiry  time.Time
}

// New creates a search Client with the given providers in fallback order.
func New(providers []Provider) *Client {
	return &Client{
		providers:  providers,
		cache:      make(map[string]cacheEntry),
		cacheTTL:   5 * time.Minute,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Search queries providers in order, returning the first successful result.
func (c *Client) Search(ctx context.Context, query string, n int) ([]Result, error) {
	if n <= 0 {
		n = 5
	}
	cacheKey := fmt.Sprintf("%s|%d", query, n)

	c.mu.RLock()
	if entry, ok := c.cache[cacheKey]; ok && time.Now().Before(entry.expiry) {
		results := entry.results
		c.mu.RUnlock()
		return results, nil
	}
	c.mu.RUnlock()

	var lastErr error
	for _, p := range c.providers {
		results, err := p.Search(ctx, query, n)
		if err == nil && len(results) > 0 {
			c.mu.Lock()
			c.cache[cacheKey] = cacheEntry{results: results, expiry: time.Now().Add(c.cacheTTL)}
			c.mu.Unlock()
			return results, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all search providers failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no results from any provider")
}

// FetchContent fetches a URL and extracts plain text.
func (c *Client) FetchContent(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Theseus/1.0)")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}
	return extractText(string(body)), nil
}

// extractText strips HTML tags and returns plain text.
func extractText(html string) string {
	// Remove script/style blocks
	for _, tag := range []string{"script", "style", "head", "nav", "footer"} {
		for {
			open := strings.Index(strings.ToLower(html), "<"+tag)
			if open == -1 {
				break
			}
			close := strings.Index(strings.ToLower(html[open:]), "</"+tag+">")
			if close == -1 {
				break
			}
			html = html[:open] + html[open+close+len("</"+tag+">"):]
		}
	}
	// Strip all remaining tags
	var sb strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			sb.WriteRune(' ')
		case !inTag:
			sb.WriteRune(r)
		}
	}
	// Collapse whitespace
	text := sb.String()
	lines := strings.Split(text, "\n")
	var clean []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			clean = append(clean, line)
		}
	}
	result := strings.Join(clean, "\n")
	if len(result) > 50_000 {
		result = result[:50_000]
	}
	return result
}

// --- SearXNG Provider ---

type SearXNGProvider struct {
	baseURL    string
	httpClient *http.Client
}

func NewSearXNG(baseURL string) *SearXNGProvider {
	return &SearXNGProvider{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *SearXNGProvider) Name() string { return "searxng" }

func (p *SearXNGProvider) Search(ctx context.Context, query string, n int) ([]Result, error) {
	u := fmt.Sprintf("%s/search?q=%s&format=json&engines=google,bing,duckduckgo",
		strings.TrimRight(p.baseURL, "/"), url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	results := make([]Result, 0, n)
	for i, r := range data.Results {
		if i >= n {
			break
		}
		results = append(results, Result{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	return results, nil
}

// --- DuckDuckGo Provider (HTML scrape) ---

type DuckDuckGoProvider struct {
	httpClient *http.Client
}

func NewDuckDuckGo() *DuckDuckGoProvider {
	return &DuckDuckGoProvider{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (p *DuckDuckGoProvider) Name() string { return "duckduckgo" }

func (p *DuckDuckGoProvider) Search(ctx context.Context, query string, n int) ([]Result, error) {
	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Theseus/1.0)")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return parseDDGHTML(string(body), n), nil
}

func parseDDGHTML(html string, n int) []Result {
	var results []Result
	// Simple extraction: find result links and snippets
	parts := strings.Split(html, `class="result__a"`)
	for i := 1; i < len(parts) && len(results) < n; i++ {
		// Extract href
		hrefStart := strings.Index(parts[i], `href="`)
		if hrefStart == -1 {
			continue
		}
		hrefStart += 6
		hrefEnd := strings.Index(parts[i][hrefStart:], `"`)
		if hrefEnd == -1 {
			continue
		}
		href := parts[i][hrefStart : hrefStart+hrefEnd]
		// Extract title text
		titleStart := strings.Index(parts[i], ">")
		titleEnd := strings.Index(parts[i], "</a>")
		title := ""
		if titleStart != -1 && titleEnd != -1 && titleEnd > titleStart {
			title = extractText(parts[i][titleStart+1 : titleEnd])
		}
		if href != "" && strings.HasPrefix(href, "http") {
			results = append(results, Result{Title: title, URL: href})
		}
	}
	return results
}

// --- Brave API Provider ---

type BraveProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewBrave(apiKey string) *BraveProvider {
	return &BraveProvider{apiKey: apiKey, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (p *BraveProvider) Name() string { return "brave" }

func (p *BraveProvider) Search(ctx context.Context, query string, n int) ([]Result, error) {
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.apiKey)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	results := make([]Result, 0, n)
	for _, r := range data.Web.Results {
		results = append(results, Result{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	return results, nil
}

// --- Google PSE Provider ---

type GooglePSEProvider struct {
	apiKey     string
	cx         string
	httpClient *http.Client
}

func NewGooglePSE(apiKey, cx string) *GooglePSEProvider {
	return &GooglePSEProvider{apiKey: apiKey, cx: cx, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (p *GooglePSEProvider) Name() string { return "google_pse" }

func (p *GooglePSEProvider) Search(ctx context.Context, query string, n int) ([]Result, error) {
	u := fmt.Sprintf("https://www.googleapis.com/customsearch/v1?key=%s&cx=%s&q=%s&num=%d",
		p.apiKey, p.cx, url.QueryEscape(query), n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(data.Items))
	for _, item := range data.Items {
		results = append(results, Result{Title: item.Title, URL: item.Link, Snippet: item.Snippet})
	}
	return results, nil
}

// --- Tavily Provider ---

type TavilyProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewTavily(apiKey string) *TavilyProvider {
	return &TavilyProvider{apiKey: apiKey, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

func (p *TavilyProvider) Name() string { return "tavily" }

func (p *TavilyProvider) Search(ctx context.Context, query string, n int) ([]Result, error) {
	body, _ := json.Marshal(map[string]any{
		"api_key":        p.apiKey,
		"query":          query,
		"max_results":    n,
		"search_depth":   "basic",
		"include_answer": false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.tavily.com/search", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(data.Results))
	for _, r := range data.Results {
		results = append(results, Result{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	return results, nil
}

// --- Serper Provider ---

type SerperProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewSerper(apiKey string) *SerperProvider {
	return &SerperProvider{apiKey: apiKey, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (p *SerperProvider) Name() string { return "serper" }

func (p *SerperProvider) Search(ctx context.Context, query string, n int) ([]Result, error) {
	body, _ := json.Marshal(map[string]any{"q": query, "num": n})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://google.serper.dev/search", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", p.apiKey)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(data.Organic))
	for _, r := range data.Organic {
		results = append(results, Result{Title: r.Title, URL: r.Link, Snippet: r.Snippet})
	}
	return results, nil
}

// BuildFromSettings constructs a Client from the current settings.
func BuildFromSettings(searchProvider, searchURL string, fallbackChain []string,
	braveKey, googleKey, googleCX, tavilyKey, serperKey string) *Client {

	providerMap := map[string]Provider{
		"duckduckgo": NewDuckDuckGo(),
	}
	if searchURL != "" {
		providerMap["searxng"] = NewSearXNG(searchURL)
	}
	if braveKey != "" {
		providerMap["brave"] = NewBrave(braveKey)
	}
	if googleKey != "" && googleCX != "" {
		providerMap["google_pse"] = NewGooglePSE(googleKey, googleCX)
	}
	if tavilyKey != "" {
		providerMap["tavily"] = NewTavily(tavilyKey)
	}
	if serperKey != "" {
		providerMap["serper"] = NewSerper(serperKey)
	}

	var providers []Provider
	// Primary provider first
	if p, ok := providerMap[searchProvider]; ok {
		providers = append(providers, p)
	}
	// Fallback chain
	for _, name := range fallbackChain {
		if name == searchProvider {
			continue
		}
		if p, ok := providerMap[name]; ok {
			providers = append(providers, p)
		}
	}
	// Always have DuckDuckGo as last resort
	hasDDG := false
	for _, p := range providers {
		if p.Name() == "duckduckgo" {
			hasDDG = true
			break
		}
	}
	if !hasDDG {
		providers = append(providers, NewDuckDuckGo())
	}

	return New(providers)
}
