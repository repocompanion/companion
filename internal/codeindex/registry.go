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
		if provider == nil {
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
