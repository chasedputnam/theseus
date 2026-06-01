package cookbook

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed data/hf_models.json
var hfModelsJSON []byte

// ModelEntry is a model from the HuggingFace catalog.
type ModelEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	VRAM        int      `json:"vram_mb"`
	RAM         int      `json:"ram_mb"`
	Format      string   `json:"format"` // "gguf", "fp8", "awq"
	Size        string   `json:"size"`   // "7B", "13B", etc.
	Backend     string   `json:"backend"` // "llama.cpp", "vllm"
	RepoID      string   `json:"repo_id"`
	Filename    string   `json:"filename"` // for GGUF
	FitScore    float64  `json:"fit_score,omitempty"`
}

// LoadCatalog returns all models from the embedded catalog.
func LoadCatalog() ([]*ModelEntry, error) {
	var models []*ModelEntry
	if err := json.Unmarshal(hfModelsJSON, &models); err != nil {
		return nil, err
	}
	return models, nil
}

// RecommendModels returns models sorted by fit score for the given hardware.
func RecommendModels(profile *HardwareProfile, filter string) ([]*ModelEntry, error) {
	models, err := LoadCatalog()
	if err != nil {
		return nil, err
	}

	var scored []*ModelEntry
	for _, m := range models {
		score := FitScore(profile, m.VRAM, m.RAM)
		if score <= 0 {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(m.Name+" "+m.Description), strings.ToLower(filter)) {
			continue
		}
		m.FitScore = score
		scored = append(scored, m)
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].FitScore > scored[j].FitScore
	})
	return scored, nil
}
