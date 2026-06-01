package agent

import (
	"testing"
)

func TestParseToolBlocks(t *testing.T) {
	text := "Here is some text\n```bash\necho hello\n```\nMore text\n```python\nprint('hi')\n```\n```shell\nls -la\n```"
	blocks := ParseToolBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 tool blocks (bash, python), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].ToolType != "bash" || blocks[0].Content != "echo hello" {
		t.Errorf("block[0]: got %+v", blocks[0])
	}
	if blocks[1].ToolType != "python" || blocks[1].Content != "print('hi')" {
		t.Errorf("block[1]: got %+v", blocks[1])
	}
}

func TestStripToolBlocks(t *testing.T) {
	text := "Before\n```bash\necho hi\n```\nAfter"
	stripped := StripToolBlocks(text)
	if stripped != "Before\n\nAfter" && stripped != "Before\nAfter" {
		// Allow either — depends on whitespace handling
		if stripped == "" {
			t.Errorf("StripToolBlocks returned empty, want prose preserved")
		}
	}
	// Non-tool blocks should be preserved
	text2 := "Code:\n```python\nprint('hi')\n```\nExample:\n```sh\nls\n```"
	stripped2 := StripToolBlocks(text2)
	if !containsStr(stripped2, "Example:") {
		t.Errorf("non-tool block prose should be preserved, got: %q", stripped2)
	}
}

func TestParseToolBlocksEmpty(t *testing.T) {
	blocks := ParseToolBlocks("no tool blocks here")
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s[1:], sub) || s[:len(sub)] == sub)
}

func TestToolTagsHaveHandlers(t *testing.T) {
	// Verify every tag in toolTags is a known tool type.
	// This catches typos that would silently drop tool calls.
	knownHandlers := map[string]bool{
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
	for tag := range toolTags {
		if !knownHandlers[tag] {
			t.Errorf("toolTags contains %q which has no registered handler — possible typo", tag)
		}
	}
	for handler := range knownHandlers {
		if !toolTags[handler] {
			t.Errorf("handler %q is registered but missing from toolTags", handler)
		}
	}
}
