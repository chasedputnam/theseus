package memory

import (
	"log"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chaseputnam/theseus/internal/storage"
)

// Skill represents a parsed SKILL.md file.
type Skill struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Category         string   `json:"category"`
	Tags             []string `json:"tags"`
	WhenToUse        string   `json:"when_to_use"`
	Procedure        []string `json:"procedure"`
	Pitfalls         []string `json:"pitfalls"`
	Verification     []string `json:"verification"`
	Status           string   `json:"status"` // "draft" or "published"
	Version          string   `json:"version"`
	Confidence       float64  `json:"confidence"`
	Author           string   `json:"author"` // "user" or "ai"
	Owner            string   `json:"owner"`
	Path             string   `json:"path"` // relative path under skills dir
}

// SkillsManager manages SKILL.md files on disk.
type SkillsManager struct {
	skillsDir string
}

// NewSkillsManager creates a SkillsManager rooted at skillsDir.
func NewSkillsManager(skillsDir string) *SkillsManager {
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		log.Printf("skills: create dir %s: %v", skillsDir, err)
	}
	return &SkillsManager{skillsDir: skillsDir}
}

// List returns all skills for an owner (or all if owner is empty).
func (sm *SkillsManager) List(owner string) ([]*Skill, error) {
	var skills []*Skill
	err := filepath.Walk(sm.skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "SKILL.md" {
			return nil
		}
		skill, err := sm.parseSkillFile(path)
		if err != nil {
			return nil // skip malformed files
		}
		if owner == "" || skill.Owner == owner || skill.Owner == "" {
			skills = append(skills, skill)
		}
		return nil
	})
	return skills, err
}

// Get returns a skill by name and category.
func (sm *SkillsManager) Get(category, name string) (*Skill, error) {
	path := filepath.Join(sm.skillsDir, category, name, "SKILL.md")
	return sm.parseSkillFile(path)
}

// Save writes a skill to disk as SKILL.md.
func (sm *SkillsManager) Save(skill *Skill) error {
	if skill.Category == "" {
		skill.Category = "general"
	}
	if skill.Name == "" {
		return fmt.Errorf("skill name required")
	}
	dir := filepath.Join(sm.skillsDir, skill.Category, sanitizeName(skill.Name))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	skill.Path = filepath.Join(skill.Category, sanitizeName(skill.Name))
	content := renderSkillMD(skill)
	return storage.WriteBytes(filepath.Join(dir, "SKILL.md"), []byte(content))
}

// Delete removes a skill directory.
func (sm *SkillsManager) Delete(category, name string) error {
	if category == "" || name == "" {
		return fmt.Errorf("category and name are required")
	}
	dir := filepath.Join(sm.skillsDir, category, sanitizeName(name))
	return os.RemoveAll(dir)
}

// Publish sets a skill's status to "published".
func (sm *SkillsManager) Publish(category, name string) error {
	skill, err := sm.Get(category, name)
	if err != nil {
		return err
	}
	skill.Status = "published"
	return sm.Save(skill)
}

// RelevantSkills returns up to maxN skills relevant to the query, filtered by confidence.
func (sm *SkillsManager) RelevantSkills(query, owner string, maxN int, minConfidence float64) []*Skill {
	all, _ := sm.List(owner)
	type scored struct {
		skill *Skill
		score float64
	}
	var candidates []scored
	queryTokens := tokenize(query)
	for _, s := range all {
		if s.Status != "published" && s.Confidence < minConfidence {
			continue
		}
		// Score by token overlap with description + when_to_use + tags
		searchText := strings.ToLower(s.Description + " " + s.WhenToUse + " " + strings.Join(s.Tags, " "))
		skillTokens := tokenize(searchText)
		score := jaccard(queryTokens, skillTokens)
		if score > 0 {
			candidates = append(candidates, scored{s, score})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]*Skill, 0, maxN)
	for i, c := range candidates {
		if i >= maxN {
			break
		}
		result = append(result, c.skill)
	}
	return result
}

// parseSkillFile reads and parses a SKILL.md file.
func (sm *SkillsManager) parseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	skill := &Skill{
		Status:     "draft",
		Confidence: 0.8,
		Author:     "user",
		Version:    "1.0.0",
	}
	// Extract relative path
	rel, _ := filepath.Rel(sm.skillsDir, filepath.Dir(path))
	skill.Path = rel

	// Parse frontmatter (--- ... ---)
	content := string(data)
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			parseFrontmatter(parts[1], skill)
			content = parts[2]
		}
	}

	// Parse body sections
	parseSkillBody(content, skill)

	// Derive category/name from path
	pathParts := strings.Split(rel, string(filepath.Separator))
	if len(pathParts) >= 2 {
		skill.Category = pathParts[0]
		if skill.Name == "" {
			skill.Name = pathParts[1]
		}
	}
	return skill, nil
}

func parseFrontmatter(fm string, skill *Skill) {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "name":
			skill.Name = val
		case "description":
			skill.Description = val
		case "category":
			skill.Category = val
		case "status":
			skill.Status = val
		case "version":
			skill.Version = val
		case "author":
			skill.Author = val
		case "owner":
			skill.Owner = val
		case "confidence":
			fmt.Sscanf(val, "%f", &skill.Confidence)
		case "tags":
			for _, t := range strings.Split(val, ",") {
				if t = strings.TrimSpace(t); t != "" {
					skill.Tags = append(skill.Tags, t)
				}
			}
		}
	}
}

func parseSkillBody(body string, skill *Skill) {
	var currentSection string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			currentSection = strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			if skill.Name == "" {
				skill.Name = strings.TrimPrefix(trimmed, "# ")
			}
			continue
		}
		switch currentSection {
		case "when to use", "when_to_use":
			if trimmed != "" {
				skill.WhenToUse += trimmed + " "
			}
		case "description":
			if trimmed != "" && skill.Description == "" {
				skill.Description = trimmed
			}
		case "procedure", "steps":
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
				skill.Procedure = append(skill.Procedure, strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "))
			}
		case "pitfalls":
			if strings.HasPrefix(trimmed, "- ") {
				skill.Pitfalls = append(skill.Pitfalls, strings.TrimPrefix(trimmed, "- "))
			}
		case "verification":
			if strings.HasPrefix(trimmed, "- ") {
				skill.Verification = append(skill.Verification, strings.TrimPrefix(trimmed, "- "))
			}
		}
	}
	skill.WhenToUse = strings.TrimSpace(skill.WhenToUse)
}

func renderSkillMD(skill *Skill) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", skill.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", skill.Description))
	sb.WriteString(fmt.Sprintf("category: %s\n", skill.Category))
	sb.WriteString(fmt.Sprintf("status: %s\n", skill.Status))
	sb.WriteString(fmt.Sprintf("version: %s\n", skill.Version))
	sb.WriteString(fmt.Sprintf("author: %s\n", skill.Author))
	sb.WriteString(fmt.Sprintf("confidence: %.2f\n", skill.Confidence))
	if skill.Owner != "" {
		sb.WriteString(fmt.Sprintf("owner: %s\n", skill.Owner))
	}
	if len(skill.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("tags: %s\n", strings.Join(skill.Tags, ", ")))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s\n\n", skill.Name))
	if skill.Description != "" {
		sb.WriteString(fmt.Sprintf("## Description\n%s\n\n", skill.Description))
	}
	if skill.WhenToUse != "" {
		sb.WriteString(fmt.Sprintf("## When To Use\n%s\n\n", skill.WhenToUse))
	}
	if len(skill.Procedure) > 0 {
		sb.WriteString("## Procedure\n")
		for _, step := range skill.Procedure {
			sb.WriteString(fmt.Sprintf("- %s\n", step))
		}
		sb.WriteString("\n")
	}
	if len(skill.Pitfalls) > 0 {
		sb.WriteString("## Pitfalls\n")
		for _, p := range skill.Pitfalls {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
		sb.WriteString("\n")
	}
	if len(skill.Verification) > 0 {
		sb.WriteString("## Verification\n")
		for _, v := range skill.Verification {
			sb.WriteString(fmt.Sprintf("- %s\n", v))
		}
	}
	return sb.String()
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
