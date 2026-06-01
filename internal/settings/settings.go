package settings

import (
	"os"
	"sync"
	"time"

	"github.com/chaseputnam/theseus/internal/storage"
)

const cacheTTL = 2 * time.Second

var (
	mu           sync.RWMutex
	settingsPath string
	featuresPath string

	settingsCache    map[string]any
	settingsCachedAt time.Time

	featuresCache    map[string]bool
	featuresCachedAt time.Time
)

// DefaultSettings mirrors Python's DEFAULT_SETTINGS.
var DefaultSettings = map[string]any{
	"image_gen_enabled":            true,
	"image_model":                  "",
	"image_quality":                "medium",
	"vision_model":                 "",
	"vision_enabled":               true,
	"vision_model_fallbacks":       []any{},
	"app_public_url":               "",
	"tts_enabled":                  true,
	"tts_provider":                 "disabled",
	"tts_model":                    "tts-1",
	"tts_voice":                    "alloy",
	"tts_speed":                    "1",
	"stt_enabled":                  false,
	"stt_provider":                 "disabled",
	"stt_model":                    "base",
	"stt_language":                 "",
	"search_provider":              "searxng",
	"search_fallback_chain":        []any{"duckduckgo"},
	"search_url":                   "",
	"search_result_count":          float64(5),
	"brave_api_key":                "",
	"google_pse_key":               "",
	"google_pse_cx":                "",
	"tavily_api_key":               "",
	"serper_api_key":               "",
	"research_endpoint_id":         "",
	"research_model":               "",
	"research_search_provider":     "",
	"research_max_tokens":          float64(16384),
	"agent_max_tool_calls":         float64(0),
	"agent_input_token_budget":     float64(6000),
	"agent_stream_timeout_seconds": float64(300),
	"task_endpoint_id":             "",
	"task_model":                   "",
	"default_endpoint_id":          "",
	"default_model":                "",
	"default_model_fallbacks":      []any{},
	"utility_endpoint_id":          "",
	"utility_model":                "",
	"utility_model_fallbacks":      []any{},
	"teacher_model":                "",
	"teacher_enabled":              false,
	"skill_autosave_min_confidence": float64(0.85),
	"skill_max_injected":           float64(3),
	"reminder_channel":             "browser",
	"reminder_llm_synthesis":       false,
	"reminder_ntfy_topic":          "Reminders",
	"reminder_email_to":            "",
	"urgent_email_prompt": "Flag as urgent: explicit deadlines, time-sensitive requests, " +
		"work-blocking issues, messages from people I report to, or anything " +
		"where a delayed reply costs money/trust.",
	"keybinds": map[string]any{
		"search":          "ctrl+k",
		"toggle_sidebar":  "ctrl+b",
		"new_session":     "ctrl+alt+n",
		"star_session":    "ctrl+alt+s",
		"delete_session":  "ctrl+alt+d",
		"admin_panel":     "ctrl+shift+u",
		"cancel":          "escape",
	},
}

// DefaultFeatures mirrors Python's DEFAULT_FEATURES.
var DefaultFeatures = map[string]bool{
	"web_search":      true,
	"deep_research":   false,
	"memory":          true,
	"document_editor": true,
	"rag":             true,
	"sensitive_filter": true,
	"gallery":         true,
}

// Init sets the file paths for settings and features.
func Init(sPath, fPath string) {
	mu.Lock()
	settingsPath = sPath
	featuresPath = fPath
	mu.Unlock()
}

// Load returns settings merged with defaults.
func Load() map[string]any {
	mu.RLock()
	if settingsCache != nil && time.Since(settingsCachedAt) < cacheTTL {
		cp := copyMap(settingsCache)
		mu.RUnlock()
		return cp
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	// Double-check after acquiring write lock
	if settingsCache != nil && time.Since(settingsCachedAt) < cacheTTL {
		return copyMap(settingsCache)
	}

	var saved map[string]any
	if err := storage.ReadJSON(settingsPath, &saved); err != nil {
		saved = map[string]any{}
	}
	merged := copyMap(DefaultSettings)
	for k, v := range saved {
		merged[k] = v
	}
	settingsCache = merged
	settingsCachedAt = time.Now()
	return copyMap(merged)
}

// Save persists settings atomically and invalidates cache.
func Save(s map[string]any) error {
	if err := storage.WriteJSON(settingsPath, s); err != nil {
		return err
	}
	mu.Lock()
	settingsCache = nil
	mu.Unlock()
	return nil
}

// Get returns a single setting value.
func Get(key string) any {
	return Load()[key]
}

// GetString returns a setting as string.
func GetString(key string) string {
	v, _ := Get(key).(string)
	return v
}

// GetBool returns a setting as bool.
func GetBool(key string) bool {
	v, _ := Get(key).(bool)
	return v
}

// GetInt returns a setting as int (JSON numbers are float64).
func GetInt(key string) int {
	v, _ := Get(key).(float64)
	return int(v)
}

// LoadFeatures returns features merged with defaults.
func LoadFeatures() map[string]bool {
	mu.RLock()
	if featuresCache != nil && time.Since(featuresCachedAt) < cacheTTL {
		cp := copyBoolMap(featuresCache)
		mu.RUnlock()
		return cp
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if featuresCache != nil && time.Since(featuresCachedAt) < cacheTTL {
		return copyBoolMap(featuresCache)
	}

	var saved map[string]bool
	if err := storage.ReadJSON(featuresPath, &saved); err != nil {
		saved = map[string]bool{}
	}
	merged := copyBoolMap(DefaultFeatures)
	for k, v := range saved {
		merged[k] = v
	}
	featuresCache = merged
	featuresCachedAt = time.Now()
	return copyBoolMap(merged)
}

// SaveFeatures persists features atomically and invalidates cache.
func SaveFeatures(f map[string]bool) error {
	if err := storage.WriteJSON(featuresPath, f); err != nil {
		return err
	}
	mu.Lock()
	featuresCache = nil
	mu.Unlock()
	return nil
}

// FeatureEnabled returns true if the named feature is enabled.
func FeatureEnabled(key string) bool {
	return LoadFeatures()[key]
}

// Invalidate clears both caches (used in tests).
func Invalidate() {
	mu.Lock()
	settingsCache = nil
	featuresCache = nil
	mu.Unlock()
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyBoolMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Env override: if DATABASE_URL env is set, return it; else default.
func DatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "data/app.db"
}
