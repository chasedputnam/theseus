package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ChromaClient is a minimal HTTP client for ChromaDB v0.4.x REST API.
type ChromaClient struct {
	baseURL    string
	httpClient *http.Client
	collection string
}

// NewChromaClient creates a ChromaDB client.
func NewChromaClient(baseURL string) *ChromaClient {
	return &ChromaClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		collection: "memories",
	}
}

func (c *ChromaClient) collectionURL() string {
	return c.baseURL + "/api/v1/collections/" + c.collection
}

// EnsureCollection creates the collection if it doesn't exist.
func (c *ChromaClient) EnsureCollection(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{
		"name":     c.collection,
		"metadata": map[string]string{"hnsw:space": "cosine"},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/collections", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 200 = created, 409 = already exists — both OK
	if resp.StatusCode != 200 && resp.StatusCode != 409 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma create collection: %d %s", resp.StatusCode, string(b))
	}
	return nil
}

// Upsert adds or updates a document in the collection.
func (c *ChromaClient) Upsert(ctx context.Context, id, text string, metadata map[string]string) error {
	meta := make(map[string]any, len(metadata))
	for k, v := range metadata {
		meta[k] = v
	}
	body, _ := json.Marshal(map[string]any{
		"ids":       []string{id},
		"documents": []string{text},
		"metadatas": []map[string]any{meta},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.collectionURL()+"/upsert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chroma upsert: %d %s", resp.StatusCode, string(b))
	}
	return nil
}

// Query performs a semantic search and returns matching IDs.
func (c *ChromaClient) Query(ctx context.Context, queryText, owner string, limit int) ([]string, error) {
	where := map[string]any{}
	if owner != "" {
		where["owner"] = owner
	}
	body, _ := json.Marshal(map[string]any{
		"query_texts": []string{queryText},
		"n_results":   limit,
		"where":       where,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.collectionURL()+"/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chroma query: %d %s", resp.StatusCode, string(b))
	}
	var result struct {
		IDs [][]string `json:"ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.IDs) == 0 {
		return nil, nil
	}
	return result.IDs[0], nil
}

// Delete removes a document from the collection.
func (c *ChromaClient) Delete(ctx context.Context, id string) error {
	body, _ := json.Marshal(map[string]any{"ids": []string{id}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.collectionURL()+"/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Ping checks if ChromaDB is reachable.
func (c *ChromaClient) Ping(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/heartbeat", nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("chroma heartbeat: %d", resp.StatusCode)
	}
	return nil
}
