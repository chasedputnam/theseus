package research

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/search"
)

// GenerateHTMLReport produces a self-contained HTML report from research results.
func GenerateHTMLReport(question, markdownReport string, sources []search.Result, generatedAt time.Time) string {
	// Convert markdown to basic HTML
	body := markdownToHTML(markdownReport)

	// Build sources list
	var sourcesHTML strings.Builder
	for i, s := range sources {
		sourcesHTML.WriteString(fmt.Sprintf(
			`<li><a href="%s" target="_blank" rel="noopener">%s</a> — %s</li>`,
			html.EscapeString(s.URL),
			html.EscapeString(s.Title),
			html.EscapeString(truncate(s.Snippet, 120)),
		))
		_ = i
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
:root { --bg: #fff; --fg: #1a1a1a; --accent: #2563eb; --border: #e5e7eb; --code-bg: #f3f4f6; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #0f172a; --fg: #e2e8f0; --accent: #60a5fa; --border: #334155; --code-bg: #1e293b; }
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, -apple-system, sans-serif; background: var(--bg); color: var(--fg); line-height: 1.7; padding: 2rem; max-width: 860px; margin: 0 auto; }
h1 { font-size: 2rem; margin-bottom: 0.5rem; color: var(--accent); }
h2 { font-size: 1.4rem; margin: 2rem 0 0.75rem; border-bottom: 1px solid var(--border); padding-bottom: 0.25rem; }
h3 { font-size: 1.1rem; margin: 1.5rem 0 0.5rem; }
p { margin: 0.75rem 0; }
ul, ol { margin: 0.75rem 0 0.75rem 1.5rem; }
li { margin: 0.25rem 0; }
a { color: var(--accent); }
code { background: var(--code-bg); padding: 0.1em 0.3em; border-radius: 3px; font-size: 0.9em; }
pre { background: var(--code-bg); padding: 1rem; border-radius: 6px; overflow-x: auto; margin: 1rem 0; }
blockquote { border-left: 3px solid var(--accent); padding-left: 1rem; color: #64748b; margin: 1rem 0; }
.meta { color: #64748b; font-size: 0.85rem; margin-bottom: 2rem; }
.sources { margin-top: 3rem; padding-top: 1rem; border-top: 1px solid var(--border); }
.sources h2 { font-size: 1rem; color: #64748b; }
.sources ul { font-size: 0.85rem; }
.sources a { word-break: break-all; }
</style>
</head>
<body>
<h1>%s</h1>
<p class="meta">Generated %s · %d sources</p>
<div class="report">%s</div>
<div class="sources">
<h2>Sources</h2>
<ul>%s</ul>
</div>
</body>
</html>`,
		html.EscapeString(question),
		html.EscapeString(question),
		generatedAt.Format("January 2, 2006 at 15:04 UTC"),
		len(sources),
		body,
		sourcesHTML.String(),
	)
}

// markdownToHTML converts a subset of markdown to HTML.
func markdownToHTML(md string) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	inCode := false
	inList := false

	for _, line := range lines {
		// Code blocks
		if strings.HasPrefix(line, "```") {
			if inCode {
				sb.WriteString("</code></pre>\n")
				inCode = false
			} else {
				lang := strings.TrimPrefix(line, "```")
				sb.WriteString(fmt.Sprintf(`<pre><code class="language-%s">`, html.EscapeString(lang)))
				inCode = true
			}
			continue
		}
		if inCode {
			sb.WriteString(html.EscapeString(line) + "\n")
			continue
		}

		// Close list if needed
		if inList && !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			sb.WriteString("</ul>\n")
			inList = false
		}

		switch {
		case strings.HasPrefix(line, "### "):
			sb.WriteString("<h3>" + inlineMarkdown(line[4:]) + "</h3>\n")
		case strings.HasPrefix(line, "## "):
			sb.WriteString("<h2>" + inlineMarkdown(line[3:]) + "</h2>\n")
		case strings.HasPrefix(line, "# "):
			sb.WriteString("<h2>" + inlineMarkdown(line[2:]) + "</h2>\n")
		case strings.HasPrefix(line, "> "):
			sb.WriteString("<blockquote>" + inlineMarkdown(line[2:]) + "</blockquote>\n")
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			if !inList {
				sb.WriteString("<ul>\n")
				inList = true
			}
			text := line[2:]
			sb.WriteString("<li>" + inlineMarkdown(text) + "</li>\n")
		case line == "":
			sb.WriteString("\n")
		default:
			sb.WriteString("<p>" + inlineMarkdown(line) + "</p>\n")
		}
	}
	if inList {
		sb.WriteString("</ul>\n")
	}
	if inCode {
		sb.WriteString("</code></pre>\n")
	}
	return sb.String()
}

// inlineMarkdown converts inline markdown (bold, italic, code, links) to HTML.
func inlineMarkdown(s string) string {
	// Escape HTML first
	s = html.EscapeString(s)
	// Bold **text**
	s = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(s, "<strong>$1</strong>")
	// Italic *text*
	s = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(s, "<em>$1</em>")
	// Inline code `code`
	s = regexp.MustCompile("`(.+?)`").ReplaceAllString(s, "<code>$1</code>")
	// Links [text](url)
	s = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`).ReplaceAllString(s,
		`<a href="$2" target="_blank" rel="noopener">$1</a>`)
	return s
}
