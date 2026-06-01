package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearXNGProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"results":[{"title":"Go Lang","url":"https://go.dev","content":"The Go programming language"}]}`)
	}))
	defer srv.Close()

	p := NewSearXNG(srv.URL)
	results, err := p.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Go Lang" {
		t.Errorf("expected 'Go Lang', got %q", results[0].Title)
	}
}

func TestFallbackChain(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `{"results":[{"title":"Fallback Result","url":"https://example.com","content":"fallback"}]}`)
	}))
	defer srv.Close()

	p1 := NewSearXNG(srv.URL)
	p2 := NewSearXNG(srv.URL)
	client := New([]Provider{p1, p2})

	results, err := client.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Search with fallback: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results from fallback provider")
	}
}

func TestCacheHit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		fmt.Fprintln(w, `{"results":[{"title":"Cached","url":"https://example.com","content":"cached result"}]}`)
	}))
	defer srv.Close()

	client := New([]Provider{NewSearXNG(srv.URL)})
	client.Search(context.Background(), "cache test", 5)
	client.Search(context.Background(), "cache test", 5)

	if callCount != 1 {
		t.Errorf("expected 1 provider call (cache hit), got %d", callCount)
	}
}

func TestExtractText(t *testing.T) {
	html := `<html><head><title>Test</title></head><body><p>Hello <b>world</b></p><script>alert('x')</script></body></html>`
	text := extractText(html)
	if !strings.Contains(text, "Hello") {
		t.Errorf("expected 'Hello' in extracted text, got: %q", text)
	}
	if strings.Contains(text, "alert") {
		t.Errorf("script content should be stripped, got: %q", text)
	}
}
