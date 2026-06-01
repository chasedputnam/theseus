package memory

import (
	"path/filepath"
	"testing"
)

func TestSkillSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	sm := NewSkillsManager(filepath.Join(dir, "skills"))

	skill := &Skill{
		Name:        "Deploy App",
		Description: "How to deploy the application",
		Category:    "devops",
		WhenToUse:   "When deploying to production",
		Procedure:   []string{"Build the binary", "Copy to server", "Restart service"},
		Status:      "published",
		Confidence:  0.95,
		Author:      "user",
		Owner:       "alice",
		Tags:        []string{"deploy", "production"},
	}
	if err := sm.Save(skill); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := sm.Get("devops", "deploy-app")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.Name != "Deploy App" {
		t.Errorf("expected 'Deploy App', got %q", loaded.Name)
	}
	if loaded.Status != "published" {
		t.Errorf("expected published, got %q", loaded.Status)
	}
	if len(loaded.Procedure) != 3 {
		t.Errorf("expected 3 procedure steps, got %d", len(loaded.Procedure))
	}
}

func TestSkillList(t *testing.T) {
	dir := t.TempDir()
	sm := NewSkillsManager(filepath.Join(dir, "skills"))

	for _, name := range []string{"Skill A", "Skill B", "Skill C"} {
		sm.Save(&Skill{Name: name, Category: "general", Status: "published", Confidence: 0.9, Owner: "alice"})
	}
	// Different owner
	sm.Save(&Skill{Name: "Skill D", Category: "general", Status: "published", Confidence: 0.9, Owner: "bob"})

	skills, err := sm.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skills) != 3 {
		t.Errorf("expected 3 skills for alice, got %d", len(skills))
	}
}

func TestSkillRelevance(t *testing.T) {
	dir := t.TempDir()
	sm := NewSkillsManager(filepath.Join(dir, "skills"))

	sm.Save(&Skill{
		Name: "Deploy App", Category: "devops",
		Description: "deploy application to production server",
		WhenToUse:   "deploying releasing production",
		Status: "published", Confidence: 0.9,
	})
	sm.Save(&Skill{
		Name: "Write Tests", Category: "testing",
		Description: "write unit tests for code",
		WhenToUse:   "testing code quality",
		Status: "published", Confidence: 0.9,
	})

	relevant := sm.RelevantSkills("deploy to production", "", 3, 0.5)
	if len(relevant) == 0 {
		t.Error("expected at least one relevant skill for 'deploy to production'")
	}
	if relevant[0].Name != "Deploy App" {
		t.Errorf("expected 'Deploy App' as top result, got %q", relevant[0].Name)
	}
}

func TestSkillDelete(t *testing.T) {
	dir := t.TempDir()
	sm := NewSkillsManager(filepath.Join(dir, "skills"))
	sm.Save(&Skill{Name: "Test Skill", Category: "general", Status: "published", Confidence: 0.9})
	if err := sm.Delete("general", "test-skill"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	skills, _ := sm.List("")
	if len(skills) != 0 {
		t.Errorf("expected 0 skills after delete, got %d", len(skills))
	}
}
