package core

import "testing"

func TestSearchFindsDocumentSkillsFromNaturalLanguage(t *testing.T) {
	registry := DefaultRegistry()
	results := registry.Search("summarize PDFs and Word files", 5)
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	seen := map[string]bool{}
	for _, result := range results {
		seen[result.Skill.Name] = true
	}
	if !seen["pdf"] {
		t.Fatalf("expected pdf in results: %#v", results)
	}
	if !seen["docx"] {
		t.Fatalf("expected docx in results: %#v", results)
	}
}

func TestSearchFindsChineseAudioWorkflow(t *testing.T) {
	registry := DefaultRegistry()
	results := registry.Search("整理会议录音并生成纪要", 3)
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].Skill.Name != "audio" {
		t.Fatalf("expected audio to rank first, got %s", results[0].Skill.Name)
	}
}
