package codeindex

import (
	"context"
	"errors"
	"strings"
)

// Registry groups optional language providers. A provider that fails to index
// is reported as a catalog warning; other providers still run.
type Registry struct {
	Providers []Provider
}

func NewRegistry(providers ...Provider) *Registry {
	return &Registry{Providers: providers}
}

func NewDefaultRegistry() *Registry {
	providers := []Provider{NewGoProvider()}
	if lsp := NewLSPProvider(); lsp != nil && lsp.Available {
		providers = append(providers, lsp)
	}
	if c := NewCtagsProvider(); c != nil && c.Available {
		providers = append(providers, c)
	}
	return &Registry{Providers: providers}
}

type pathSupporter interface {
	SupportsPath(string) bool
}

func (r *Registry) ProvidersForPath(p string) []Provider {
	if r == nil {
		return nil
	}
	var out []Provider
	for _, provider := range r.Providers {
		if provider == nil {
			continue
		}
		if ps, ok := provider.(pathSupporter); ok && !ps.SupportsPath(p) {
			continue
		}
		out = append(out, provider)
	}
	return out
}

func (r *Registry) Index(ctx context.Context, req IndexRequest) (*Catalog, error) {
	if r == nil || len(r.Providers) == 0 {
		return nil, ErrUnsupported
	}
	cat := &Catalog{Repo: req.Repo, Commit: req.Commit}
	for _, provider := range r.Providers {
		if provider == nil || !provider.Capabilities().Index {
			continue
		}
		snap, err := provider.Index(ctx, req)
		if err != nil {
			if errors.Is(err, ErrUnsupported) || errors.Is(err, ErrCommitMismatch) {
				cat.Warnings = append(cat.Warnings, provider.Name()+": "+err.Error())
				continue
			}
			cat.Warnings = append(cat.Warnings, provider.Name()+": "+err.Error())
			continue
		}
		cat.Snapshots = append(cat.Snapshots, snap)
	}
	if len(cat.Snapshots) == 0 {
		return cat, ErrUnsupported
	}
	// When the Go provider indexed a file, prefer its semantic symbols over
	// ctags' syntax duplicates. ctags still covers non-Go files.
	goPaths := map[string]bool{}
	for _, snap := range cat.Snapshots {
		if snap.Provider != "go" {
			continue
		}
		for _, sym := range snap.Symbols {
			goPaths[sym.Path] = true
		}
	}
	for i, snap := range cat.Snapshots {
		if snap.Provider != "ctags" || len(goPaths) == 0 {
			continue
		}
		filtered := make([]Symbol, 0, len(snap.Symbols))
		for _, sym := range snap.Symbols {
			if strings.HasSuffix(strings.ToLower(sym.Path), ".go") && goPaths[sym.Path] {
				continue
			}
			filtered = append(filtered, sym)
		}
		copySnap := *snap
		copySnap.Symbols = filtered
		cat.Snapshots[i] = &copySnap
	}
	return cat, nil
}

func (r *Registry) Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error) {
	providers := r.ProvidersForPath(req.Path)
	if len(providers) == 0 {
		return nil, ErrUnsupported
	}
	missing := false
	var lastErr error
	for _, provider := range providers {
		sym, err := provider.Resolve(ctx, req)
		if err == nil {
			return sym, nil
		}
		if errors.Is(err, ErrSymbolNotFound) {
			missing = true
			continue
		}
		lastErr = err
	}
	if missing {
		return nil, ErrSymbolNotFound
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (r *Registry) References(ctx context.Context, req ResolveRequest, includeDeclaration bool) (*ReferenceResult, error) {
	providers := r.ProvidersForPath(req.Path)
	if len(providers) == 0 {
		return nil, ErrUnsupported
	}
	missing := false
	var lastErr error
	for _, provider := range providers {
		rp, ok := provider.(ReferenceProvider)
		if !ok {
			continue
		}
		locs, err := rp.References(ctx, req, includeDeclaration)
		if err == nil {
			return &ReferenceResult{Provider: provider.Name(), Locations: locs}, nil
		}
		if errors.Is(err, ErrSymbolNotFound) {
			missing = true
			continue
		}
		lastErr = err
	}
	if missing {
		return nil, ErrSymbolNotFound
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnsupported
}

func (r *Registry) ProvidersForLanguage(language string) []Provider {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return nil
	}
	var out []Provider
	for _, provider := range r.Providers {
		for _, l := range provider.Languages() {
			if strings.EqualFold(l, language) {
				out = append(out, provider)
				break
			}
		}
	}
	return out
}
