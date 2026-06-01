package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chaseputnam/theseus/internal/db"
	"github.com/google/uuid"
)
// Package-level compiled regexes for document parsing and language detection.
var (
	docFindReplaceRe = regexp.MustCompile(`(?s)<<<FIND>>>(.*?)<<<REPLACE>>>(.*?)<<<END>>>`)
	docTitleRe       = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	docLangRe        = regexp.MustCompile(`(?s)<language>(.*?)</language>`)
	docContentRe     = regexp.MustCompile(`(?s)<content>(.*?)</content>`)
	sniffPythonRe    = regexp.MustCompile(`(?m)^\s*(def \w|class \w|import \w|from \w[\w.]* import )`)
	sniffJSRe        = regexp.MustCompile(`(?m)^\s*(function \w|const \w|let \w|export |import .* from )`)
	sniffSQLRe       = regexp.MustCompile(`(?mi)^\s*(select .* from |create table |insert into |update \w)`)
	sniffCSSRe       = regexp.MustCompile(`(?m)^[.#]?[\w-]+\s*\{[^{}]*:[^{}]*;`)
)



// DocumentStore is the subset of db.DB used by document tools.
type DocumentStore interface {
	CreateDocument(d *db.Document) error
	GetDocument(id string) (*db.Document, error)
	UpdateDocument(d *db.Document) error
	AddDocumentVersion(v *db.DocumentVersion) error
	ListDocuments(owner string, includeArchived bool) ([]*db.Document, error)
	DeleteDocument(id string) error
}

// DoCreateDocument creates a new document from a fenced block body.
// Format: line 1 = title, optional line 2 = language, rest = content.
// Also supports <title>...</title><language>...</language><content>...</content> XML tags.
func DoCreateDocument(ctx context.Context, body string, sessionID, owner string, store DocumentStore) (string, error) {
	title, language, content := parseDocumentBlock(body)
	if title == "" {
		title = "Untitled"
	}
	if language == "" {
		language = sniffLanguage(content)
	}

	doc := &db.Document{
		ID:             uuid.New().String(),
		Title:          title,
		Language:       sql.NullString{String: language, Valid: language != ""},
		CurrentContent: content,
		VersionCount:   1,
		IsActive:       true,
		Owner:          sql.NullString{String: owner, Valid: owner != ""},
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if sessionID != "" {
		doc.SessionID = sql.NullString{String: sessionID, Valid: true}
	}

	if err := store.CreateDocument(doc); err != nil {
		return "", fmt.Errorf("create document: %w", err)
	}

	// Create initial version
	ver := &db.DocumentVersion{
		ID:            uuid.New().String(),
		DocumentID:    doc.ID,
		VersionNumber: 1,
		Content:       content,
		Source:        "ai",
		CreatedAt:     time.Now().UTC(),
	}
	if err := store.AddDocumentVersion(ver); err != nil {
		return "", fmt.Errorf("save version: %w", err)
	}

	return fmt.Sprintf("Document created: %q (id=%s, lang=%s)", title, doc.ID, language), nil
}

// DoEditDocument applies FIND/REPLACE blocks to an existing document.
// Format: document_id on first line, then FIND/REPLACE pairs.
func DoEditDocument(ctx context.Context, body string, store DocumentStore) (string, error) {
	lines := strings.SplitN(body, "\n", 2)
	if len(lines) < 2 {
		return "", fmt.Errorf("edit_document: expected document_id on first line")
	}
	docID := strings.TrimSpace(lines[0])
	rest := lines[1]

	doc, err := store.GetDocument(docID)
	if err != nil {
		return "", fmt.Errorf("document not found: %s", docID)
	}

	// Parse FIND/REPLACE blocks
	type pair struct{ find, replace string }
	var pairs []pair
	findRe := docFindReplaceRe
	matches := findRe.FindAllStringSubmatch(rest, -1)
	for _, m := range matches {
		pairs = append(pairs, pair{
			find:    strings.TrimSpace(m[1]),
			replace: strings.TrimSpace(m[2]),
		})
	}

	if len(pairs) == 0 {
		return "", fmt.Errorf("edit_document: no FIND/REPLACE blocks found")
	}

	content := doc.CurrentContent
	editCount := 0
	for _, p := range pairs {
		if strings.Contains(content, p.find) {
			content = strings.Replace(content, p.find, p.replace, 1)
			editCount++
		}
	}

	if editCount == 0 {
		return "", fmt.Errorf("edit_document: no FIND block matched any content in the document")
	}

	doc.CurrentContent = content
	doc.VersionCount++
	doc.UpdatedAt = time.Now().UTC()
	if err := store.UpdateDocument(doc); err != nil {
		return "", fmt.Errorf("update document: %w", err)
	}

	ver := &db.DocumentVersion{
		ID:            uuid.New().String(),
		DocumentID:    doc.ID,
		VersionNumber: doc.VersionCount,
		Content:       content,
		Summary:       sql.NullString{String: fmt.Sprintf("%d edit(s)", editCount), Valid: true},
		Source:        "ai",
		CreatedAt:     time.Now().UTC(),
	}
	if err := store.AddDocumentVersion(ver); err != nil {
		return "", fmt.Errorf("save version: %w", err)
	}

	return fmt.Sprintf("Document edited: v%d, %d edit(s)", doc.VersionCount, editCount), nil
}

// DoUpdateDocument replaces the full content of a document.
func DoUpdateDocument(ctx context.Context, body string, store DocumentStore) (string, error) {
	lines := strings.SplitN(body, "\n", 2)
	if len(lines) < 2 {
		return "", fmt.Errorf("update_document: expected document_id on first line")
	}
	docID := strings.TrimSpace(lines[0])
	newContent := lines[1]

	doc, err := store.GetDocument(docID)
	if err != nil {
		return "", fmt.Errorf("document not found: %s", docID)
	}

	doc.CurrentContent = newContent
	doc.VersionCount++
	doc.UpdatedAt = time.Now().UTC()
	if err := store.UpdateDocument(doc); err != nil {
		return "", fmt.Errorf("update document: %w", err)
	}

	ver := &db.DocumentVersion{
		ID:            uuid.New().String(),
		DocumentID:    doc.ID,
		VersionNumber: doc.VersionCount,
		Content:       newContent,
		Source:        "ai",
		CreatedAt:     time.Now().UTC(),
	}
	if err := store.AddDocumentVersion(ver); err != nil {
		return "", fmt.Errorf("save version: %w", err)
	}

	return fmt.Sprintf("Document updated: v%d (%d chars)", doc.VersionCount, utf8.RuneCountInString(newContent)), nil
}

// DoManageDocuments handles list/archive/delete operations.
func DoManageDocuments(ctx context.Context, argsJSON string, owner string, store DocumentStore) (string, error) {
	var args struct {
		Action     string `json:"action"`
		DocumentID string `json:"document_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	switch args.Action {
	case "list":
		docs, err := store.ListDocuments(owner, false)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for _, d := range docs {
			sb.WriteString(fmt.Sprintf("- %s (id=%s, lang=%s)\n", d.Title, d.ID, d.Language.String))
		}
		return sb.String(), nil
	case "archive":
		doc, err := store.GetDocument(args.DocumentID)
		if err != nil {
			return "", fmt.Errorf("document not found")
		}
		doc.Archived = true
		return "Document archived", store.UpdateDocument(doc)
	case "delete":
		return "Document deleted", store.DeleteDocument(args.DocumentID)
	default:
		return "", fmt.Errorf("unknown action: %s", args.Action)
	}
}

// parseDocumentBlock parses the create_document fenced block body.
func parseDocumentBlock(body string) (title, language, content string) {
	// Try XML-style tags first
	titleRe := docTitleRe
	langRe := docLangRe
	contentRe := docContentRe

	if m := titleRe.FindStringSubmatch(body); m != nil {
		title = strings.TrimSpace(m[1])
	}
	if m := langRe.FindStringSubmatch(body); m != nil {
		language = strings.TrimSpace(m[1])
	}
	if m := contentRe.FindStringSubmatch(body); m != nil {
		content = strings.TrimSpace(m[1])
		return
	}

	// Line-based: line 1 = title, optional line 2 = language keyword, rest = content
	lines := strings.SplitN(body, "\n", 3)
	if len(lines) == 0 {
		return
	}
	title = strings.TrimSpace(lines[0])
	if len(lines) == 1 {
		return
	}
	// Check if line 2 looks like a language identifier
	knownLangs := map[string]bool{
		"markdown": true, "python": true, "javascript": true, "typescript": true,
		"go": true, "rust": true, "java": true, "c": true, "cpp": true,
		"html": true, "css": true, "json": true, "yaml": true, "toml": true,
		"sql": true, "bash": true, "sh": true, "text": true, "email": true,
		"svg": true, "xml": true,
	}
	line2 := strings.TrimSpace(lines[1])
	if knownLangs[strings.ToLower(line2)] {
		language = strings.ToLower(line2)
		if len(lines) > 2 {
			content = lines[2]
		}
	} else {
		content = strings.Join(lines[1:], "\n")
	}
	return
}

// sniffLanguage detects document language from content.
func sniffLanguage(text string) string {
	s := strings.TrimSpace(text)
	if s == "" {
		return "markdown"
	}
	head := s
	if len(head) > 600 {
		head = head[:600]
	}
	hl := strings.ToLower(head)

	if strings.Contains(hl, "<svg") {
		return "svg"
	}
	if strings.HasPrefix(hl, "<?xml") {
		return "xml"
	}
	if strings.HasPrefix(hl, "<!doctype html") || strings.HasPrefix(hl, "<html") {
		return "html"
	}
	if (s[0] == '{' || s[0] == '[') {
		var v interface{}
		if json.Unmarshal([]byte(s), &v) == nil {
			return "json"
		}
	}
	if sniffPythonRe.MatchString(s) {
		return "python"
	}
	if sniffJSRe.MatchString(s) {
		return "javascript"
	}
	if sniffSQLRe.MatchString(s) {
		return "sql"
	}
	return "markdown"
}
