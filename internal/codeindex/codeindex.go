// Package codeindex validates and indexes code references from companion
// documents. It provides path validation, a grep fallback, a native Go provider
// and an optional Universal Ctags provider.
package codeindex

import (
	"context"
	"errors"
)

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
	Commit            string `json:"resolved_commit,omitempty"`
	PathStatus        string `json:"path_status"`
	SymbolStatus      string `json:"symbol_status"`
	DriftStatus       string `json:"drift_status"`
	Provider          string `json:"provider,omitempty"`
	Reason            string `json:"reason,omitempty"`
	DriftReason       string `json:"drift_reason,omitempty"`
	CodeChangedAt     string `json:"code_changed_at,omitempty"`
	ResolvedPath      string `json:"resolved_path,omitempty"`
	ResolvedLine      int    `json:"resolved_line,omitempty"`
	ResolvedQualified string `json:"resolved_qualified,omitempty"`
	ResolvedSignature string `json:"resolved_signature,omitempty"`
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
	FreshRefs    int `json:"fresh_refs"`
	StaleRefs    int `json:"stale_refs"`
	UnknownDrift int `json:"unknown_drift"`
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

	DriftFresh   = "fresh"
	DriftStale   = "stale"
	DriftUnknown = "unknown"
)

type Capabilities struct {
	Index       bool `json:"index"`
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
	Repo      string   `json:"repo"`
	Commit    string   `json:"commit"`
	Provider  string   `json:"provider"`
	Version   string   `json:"provider_version"`
	IndexedAt string   `json:"indexed_at"`
	Symbols   []Symbol `json:"symbols"`
}

type Symbol struct {
	Provider    string `json:"provider,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Package     string `json:"package,omitempty"`
	Name        string `json:"name"`
	Qualified   string `json:"qualified"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Line        int    `json:"line,omitempty"`
	Signature   string `json:"signature,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type ResolveRequest struct {
	Repo   string
	Root   string
	Commit string
	Path   string
	Symbol string
}

type Catalog struct {
	Repo      string      `json:"repo"`
	Commit    string      `json:"commit"`
	Snapshots []*Snapshot `json:"snapshots"`
	Warnings  []string    `json:"warnings,omitempty"`
}

var (
	ErrUnsupported     = errors.New("code index does not support this repository")
	ErrCommitMismatch  = errors.New("code index checkout does not match the requested commit")
	ErrSymbolNotFound  = errors.New("symbol not found")
	ErrSymbolAmbiguous = errors.New("symbol is ambiguous")
)

type Provider interface {
	Name() string
	Version() string
	Languages() []string
	Capabilities() Capabilities
	Index(ctx context.Context, req IndexRequest) (*Snapshot, error)
	Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error)
}
