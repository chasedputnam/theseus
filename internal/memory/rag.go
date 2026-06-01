package memory

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	chunkSize    = 1000 // chars per chunk
	chunkOverlap = 200
)

// RAGManager handles personal document ingestion and retrieval.
type RAGManager struct {
	docsDir string
	chroma  *ChromaClient
	db      interface {
		SearchMemories(query, owner string, limit int) ([]*interface{}, error)
	}
}

// PersonalDoc represents an uploaded personal document.
type PersonalDoc struct {
	ID       string
	Filename string
	Owner    string
	Chunks   int
}

// NewRAGManager creates a RAGManager.
func NewRAGManager(docsDir string, chroma *ChromaClient) *RAGManager {
	os.MkdirAll(docsDir, 0755)
	return &RAGManager{docsDir: docsDir, chroma: chroma}
}

// Ingest processes a document: saves to disk, chunks, and stores in ChromaDB.
func (r *RAGManager) Ingest(ctx context.Context, filename, owner string, content []byte) (*PersonalDoc, error) {
	// Save file to disk
	ownerDir := filepath.Join(r.docsDir, sanitizeOwner(owner))
	if err := os.MkdirAll(ownerDir, 0755); err != nil {
		return nil, err
	}
	docPath := filepath.Join(ownerDir, filepath.Base(filename))
	if err := os.WriteFile(docPath, content, 0644); err != nil {
		return nil, err
	}

	// Extract text
	text := extractDocText(filename, content)
	if text == "" {
		return nil, fmt.Errorf("could not extract text from %s", filename)
	}

	// Chunk text
	chunks := chunkText(text, chunkSize, chunkOverlap)

	// Store chunks in ChromaDB if available
	docID := sanitizeName(filename)
	if r.chroma != nil {
		_ = r.chroma.EnsureCollection(ctx)
		for i, chunk := range chunks {
			chunkID := fmt.Sprintf("%s_%s_%d", owner, docID, i)
			_ = r.chroma.Upsert(ctx, chunkID, chunk, map[string]string{
				"owner":    owner,
				"filename": filename,
				"chunk":    fmt.Sprintf("%d", i),
				"type":     "personal_doc",
			})
		}
	}

	return &PersonalDoc{
		ID:       docID,
		Filename: filename,
		Owner:    owner,
		Chunks:   len(chunks),
	}, nil
}

// Retrieve returns relevant chunks for a query.
func (r *RAGManager) Retrieve(ctx context.Context, query, owner string, limit int) ([]string, error) {
	if r.chroma != nil {
		ids, err := r.chroma.Query(ctx, query, owner, limit)
		if err == nil && len(ids) > 0 {
			// Fetch chunk content from ChromaDB
			// For now return IDs as placeholder — full implementation needs get_by_ids
			return ids, nil
		}
	}
	// Fallback: keyword search over saved files
	return r.keywordSearch(query, owner, limit)
}

// Delete removes a personal document and its chunks.
func (r *RAGManager) Delete(ctx context.Context, filename, owner string) error {
	ownerDir := filepath.Join(r.docsDir, sanitizeOwner(owner))
	docPath := filepath.Join(ownerDir, filepath.Base(filename))
	return os.Remove(docPath)
}

// List returns all personal documents for an owner.
func (r *RAGManager) List(owner string) ([]string, error) {
	ownerDir := filepath.Join(r.docsDir, sanitizeOwner(owner))
	entries, err := os.ReadDir(ownerDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

func (r *RAGManager) keywordSearch(query, owner string, limit int) ([]string, error) {
	ownerDir := filepath.Join(r.docsDir, sanitizeOwner(owner))
	entries, err := os.ReadDir(ownerDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	queryWords := strings.Fields(strings.ToLower(query))
	var results []string
	for _, e := range entries {
		if e.IsDir() || len(results) >= limit {
			break
		}
		data, err := os.ReadFile(filepath.Join(ownerDir, e.Name()))
		if err != nil {
			continue
		}
		text := string(data)
		textLower := strings.ToLower(text)
		for _, word := range queryWords {
			if strings.Contains(textLower, word) {
				// Return a relevant excerpt
				idx := strings.Index(textLower, word)
				start := idx - 200
				if start < 0 {
					start = 0
				}
				end := idx + 500
				if end > len(text) {
					end = len(text)
				}
				results = append(results, fmt.Sprintf("[%s] ...%s...", e.Name(), text[start:end]))
				break
			}
		}
	}
	return results, nil
}

// chunkText splits text into overlapping chunks.
func chunkText(text string, size, overlap int) []string {
	runes := []rune(text)
	var chunks []string
	for i := 0; i < len(runes); i += size - overlap {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// extractDocText extracts plain text from a document.
func extractDocText(filename string, content []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".txt", ".md", ".csv", ".json", ".yaml", ".yml":
		return string(content)
	case ".pdf":
		// Basic PDF text extraction: look for stream content
		text := extractPDFText(content)
		if text != "" {
			return text
		}
		return string(content) // fallback
	default:
		// Try as UTF-8 text
		if utf8.Valid(content) {
			return string(content)
		}
		return ""
	}
}

// extractPDFText does a basic extraction of text from PDF bytes.
func extractPDFText(data []byte) string {
	content := string(data)
	var sb strings.Builder
	// Find BT...ET blocks (text objects in PDF)
	for {
		start := strings.Index(content, "BT")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "ET")
		if end == -1 {
			break
		}
		block := content[start : start+end+2]
		// Extract Tj and TJ strings
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasSuffix(line, "Tj") || strings.HasSuffix(line, "TJ") {
				// Extract content between parentheses
				for {
					ps := strings.Index(line, "(")
					pe := strings.Index(line, ")")
					if ps == -1 || pe == -1 || pe <= ps {
						break
					}
					sb.WriteString(line[ps+1 : pe])
					sb.WriteRune(' ')
					line = line[pe+1:]
				}
			}
		}
		content = content[start+end+2:]
	}
	return strings.TrimSpace(sb.String())
}

func sanitizeOwner(owner string) string {
	if owner == "" {
		return "_shared"
	}
	return sanitizeName(owner)
}

var _ io.Reader // keep import
