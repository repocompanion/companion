package codeindex

import "testing"

func TestPreflightClassifiesErrorsAndWarnings(t *testing.T) {
	docs := []DocValidation{{
		Path: "projects/acme/api/patterns/x.md",
		References: []ReferenceValidation{
			{Reference: Reference{Path: "internal/missing.go"}, PathStatus: PathMissing, SymbolStatus: SymbolUnverified, Commit: "0123456789abcdef"},
			{Reference: Reference{Path: "internal/consumer.go", Symbol: "Consumer.Missing"}, PathStatus: PathValid, SymbolStatus: SymbolMissing, Provider: "go"},
			{Reference: Reference{Path: "internal/consumer.go", Symbol: "Consumer.Run"}, PathStatus: PathValid, SymbolStatus: SymbolValid, Provider: "go", DriftStatus: DriftStale, DriftReason: "test"},
			{Reference: Reference{Path: "internal/consumer.go"}, PathStatus: PathValid, SymbolStatus: SymbolNotRequested, DriftStatus: DriftStale, DriftReason: "test"},
			{Reference: Reference{Path: "internal/consumer.go"}, PathStatus: PathExternal, SymbolStatus: SymbolExternal},
		},
	}}
	issues := Preflight(docs)
	if len(issues) != 5 {
		t.Fatalf("issues = %+v", issues)
	}
	var errors, warnings int
	for _, i := range issues {
		switch i.Level {
		case "error":
			errors++
		case "warning":
			warnings++
		}
	}
	if errors != 2 || warnings != 3 {
		t.Fatalf("errors=%d warnings=%d issues=%+v", errors, warnings, issues)
	}
	if !HasPreflightErrors(issues) {
		t.Fatal("expected blocking issues")
	}
}
