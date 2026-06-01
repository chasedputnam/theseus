package agent

import (
	"regexp"
	"strings"
)

// ToolBlock represents a parsed fenced code block tool call.
type ToolBlock struct {
	ToolType string
	Content  string
}

// toolTags is the set of fenced block language tags that trigger tool execution.
var toolTags = map[string]bool{
	"bash": true, "python": true, "web_search": true,
	"read_file": true, "write_file": true,
	"create_document": true, "update_document": true, "edit_document": true,
	"search_chats": true,
	"chat_with_model": true, "create_session": true, "list_sessions": true,
	"send_to_session": true, "pipeline": true,
	"manage_session": true, "manage_memory": true, "list_models": true,
	"ui_control": true, "generate_image": true,
	"manage_tasks": true, "api_call": true, "ask_teacher": true, "manage_skills": true,
	"suggest_document": true,
	"manage_endpoints": true, "manage_mcp": true, "manage_webhooks": true,
	"manage_tokens": true, "manage_documents": true, "manage_settings": true,
	"manage_notes": true, "manage_calendar": true,
	"resolve_contact": true, "manage_contact": true,
	"list_email_accounts": true, "send_email": true, "list_emails": true,
	"read_email": true, "reply_to_email": true, "bulk_email": true,
	"archive_email": true, "delete_email": true, "mark_email_read": true,
	"download_model": true, "serve_model": true,
	"list_served_models": true, "stop_served_model": true,
	"list_downloads": true, "cancel_download": true,
	"search_hf_models": true, "list_cached_models": true,
	"list_serve_presets": true, "serve_preset": true, "adopt_served_model": true,
	"list_cookbook_servers": true,
	"edit_image": true, "trigger_research": true, "manage_research": true,
	"app_api": true,
}

// fencedBlockRe matches ```toolname\ncontent\n``` blocks.
var fencedBlockRe = regexp.MustCompile(`(?s)` + "`" + `(\w+)\n(.*?)\n?` + "`" + ``)

// ParseToolBlocks extracts all tool blocks from LLM output.
func ParseToolBlocks(text string) []ToolBlock {
	matches := fencedBlockRe.FindAllStringSubmatch(text, -1)
	var blocks []ToolBlock
	for _, m := range matches {
		tag := strings.ToLower(m[1])
		if toolTags[tag] {
			blocks = append(blocks, ToolBlock{ToolType: tag, Content: strings.TrimSpace(m[2])})
		}
	}
	return blocks
}

// StripToolBlocks removes all tool fenced blocks from text, leaving prose.
func StripToolBlocks(text string) string {
	return strings.TrimSpace(fencedBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := fencedBlockRe.FindStringSubmatch(match)
		if sub != nil && toolTags[strings.ToLower(sub[1])] {
			return ""
		}
		return match
	}))
}
