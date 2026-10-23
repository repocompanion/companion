// Package codeindex validates and indexes code references from docsync
// documents. Part 1 validates paths and provides a grep fallback; language
// providers add symbol and semantic resolution later.
package codeindex

import "context"

type Reference struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Symbol      string `json:"symbol,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type ReferenceValidation struct {
	Reference
	Commit       string `json:"resolved_commit,omitempty"`
	PathStatus   string `json:"path_status"`
	SymbolStatus string `json:"symbol_status"`
	Provider     string `json:"provider,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type DocValidation struct {
	Path       string                `json:"path"`
	References []ReferenceValidation `json:"references,omitempty"`
}

type Summary struct {
	Documents    int `json:"documents"`
	References   int `json:"references"`
	ValidPaths   int `json:"valid_paths"`
	MissingPaths int `json:"missing_paths"`
	ExternalRefs int `json:"external_refs"`
	Unverified   int `json:"unverified"`
	GrepMatches  int `json:"grep_matches"`
	Unsupported  int `json:"unsupported"`
}

const (
	PathValid      = "valid"
	PathMissing    = "missing"
	PathExternal   = "external"
	PathUnverified = "unverified"
	PathInvalid    = "invalid"

	SymbolNotRequested = "not_requested"
	SymbolValid        = "valid"
	SymbolMissing      = "missing"
	SymbolExternal     = "external"
	SymbolUnsupported  = "unsupported"
	SymbolUnverified   = "unverified"
)

type Capabilities struct {
	Paths       bool `json:"paths"`
	Symbols     bool `json:"symbols"`
	Definitions bool `json:"definitions"`
	References  bool `json:"references"`
	Fingerprint bool `json:"fingerprint"`
}

type IndexRequest struct {
	Repo   string
	Root   string
	Commit string
}

type Snapshot struct {
	Repo    string
	Commit  string
	Symbols []Symbol
}

type Symbol struct {
	Repo        string
	Commit      string
	Package     string
	Name        string
	Qualified   string
	Kind        string
	Path        string
	Line        int
	Signature   string
	Fingerprint string
}

type ResolveRequest struct {
	Repo   string
	Root   string
	Commit string
	Path   string
	Symbol string
}

// Provider is the future contract for language-specific indexing. Part 1 only
// uses the git path/grep validator; Go, ctags, LSP and SCIP providers will
// implement this interface without changing the validation result model.
type Provider interface {
	Name() string
	Version() string
	Languages() []string
	Capabilities() Capabilities
	Index(ctx context.Context, req IndexRequest) (*Snapshot, error)
	Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error)
}
