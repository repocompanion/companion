package codeindex

import "fmt"

// PreflightIssue is a submit-time issue derived from a validated reference.
// Errors should block submit; warnings are advisory and let the author decide.
type PreflightIssue struct {
	Level   string `json:"level"`
	Doc     string `json:"doc"`
	Path    string `json:"path,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Message string `json:"message"`
}

// Preflight converts code-reference validation into submit issues. Path
// validity is universal, so missing/invalid paths are errors. Symbol errors are
// only raised when a provider actually claimed support and could not find the
// symbol. Drift and provider uncertainty stay advisory.
func Preflight(docs []DocValidation) []PreflightIssue {
	var out []PreflightIssue
	for _, d := range docs {
		for _, r := range d.References {
			base := PreflightIssue{Doc: d.Path, Path: r.Path, Symbol: r.Symbol}
			switch r.PathStatus {
			case PathMissing:
				base.Level = "error"
				base.Message = fmt.Sprintf("reference path %s does not exist at %s", r.Path, shortCommit(r.Commit))
				out = append(out, base)
				continue
			case PathInvalid:
				base.Level = "error"
				base.Message = "reference path is invalid: " + r.Reason
				out = append(out, base)
				continue
			case PathExternal:
				base.Level = "warning"
				base.Message = "reference repo is external; path and symbol were not checked"
				out = append(out, base)
				continue
			case PathUnverified:
				base.Level = "warning"
				base.Message = "reference path could not be verified: " + r.Reason
				out = append(out, base)
				continue
			}
			if r.Symbol == "" {
				if r.DriftStatus == DriftStale {
					base.Level = "warning"
					base.Message = "referenced code is stale: " + r.DriftReason
					out = append(out, base)
				}
				continue
			}
			switch r.SymbolStatus {
			case SymbolMissing:
				base.Level = "error"
				base.Message = fmt.Sprintf("symbol %s was not found by %s", r.Symbol, r.Provider)
				out = append(out, base)
				continue
			case SymbolUnsupported, SymbolUnverified, SymbolExternal:
				base.Level = "warning"
				base.Message = fmt.Sprintf("symbol %s is %s; continuing with a warning", r.Symbol, r.SymbolStatus)
				out = append(out, base)
			}
			if r.DriftStatus == DriftStale {
				base.Level = "warning"
				base.Message = "referenced code is stale: " + r.DriftReason
				out = append(out, base)
			}
		}
	}
	return out
}

// HasPreflightErrors reports whether any issue should block submit.
func HasPreflightErrors(issues []PreflightIssue) bool {
	for _, i := range issues {
		if i.Level == "error" {
			return true
		}
	}
	return false
}
