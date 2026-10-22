package forge

import (
	"strings"
	"testing"
)

func TestManagedBodyPreservesHumanText(t *testing.T) {
	body := ManagedBody("Please review this.\n", "cs-1", "org/patterns", []string{"org/patterns/a.md"})
	if !strings.Contains(body, "Please review this.") {
		t.Fatal("human text lost")
	}
	if !strings.Contains(body, "Docsync-Area: org/patterns") {
		t.Fatal("managed area missing")
	}
	updated := ManagedBody(body, "cs-1", "org/patterns", []string{"org/patterns/b.md"})
	if !strings.Contains(updated, "Please review this.") {
		t.Fatal("human text lost on update")
	}
	if strings.Count(updated, bodyBegin) != 1 || strings.Count(updated, bodyEnd) != 1 {
		t.Fatalf("managed block duplicated: %s", updated)
	}
	if !strings.Contains(updated, "org/patterns/b.md") || strings.Contains(updated, "org/patterns/a.md") {
		t.Fatalf("managed file list not replaced: %s", updated)
	}
}
