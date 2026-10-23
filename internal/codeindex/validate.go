package codeindex

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/store"
)

type Validator struct {
	Root    string
	Project string
	Go      *GoProvider
}

// ValidateDocs validates every reference on the supplied documents.
func ValidateDocs(ctx context.Context, root, project string, docs []*store.Doc) ([]DocValidation, error) {
	v := Validator{Root: root, Project: project, Go: NewGoProvider()}
	out := make([]DocValidation, 0, len(docs))
	for _, d := range docs {
		if d == nil || len(d.Frontmatter.ReferenceImpls) == 0 {
			continue
		}
		dv, err := v.ValidateDoc(ctx, d)
		if err != nil {
			return nil, err
		}
		out = append(out, dv)
	}
	return out, nil
}

func (v Validator) ValidateDoc(ctx context.Context, doc *store.Doc) (DocValidation, error) {
	if doc == nil {
		return DocValidation{}, fmt.Errorf("document is nil")
	}
	out := DocValidation{Path: doc.Path}
	for _, r := range doc.Frontmatter.ReferenceImpls {
		rv := v.validateRef(ctx, referenceFromStore(r), doc.Frontmatter.LastReviewed)
		out.References = append(out.References, rv)
	}
	return out, nil
}

func (v Validator) validateRef(ctx context.Context, r Reference, lastReviewed string) ReferenceValidation {
	out := ReferenceValidation{Reference: r, SymbolStatus: SymbolNotRequested, DriftStatus: DriftUnknown}
	if strings.TrimSpace(r.Path) == "" {
		out.PathStatus = PathInvalid
		out.SymbolStatus = SymbolUnverified
		out.Reason = "reference path is empty"
		return out
	}
	cleanPath, ok := cleanRefPath(r.Path)
	if !ok {
		out.PathStatus = PathInvalid
		out.SymbolStatus = SymbolUnverified
		out.Reason = "reference path escapes the repository"
		return out
	}
	r.Path = cleanPath
	out.Reference = r

	project := strings.TrimSpace(v.Project)
	if project == "" && v.Root != "" {
		project = gitx.Detect(ctx, v.Root).Project
	}
	if strings.TrimSpace(v.Root) == "" {
		out.PathStatus = PathUnverified
		out.SymbolStatus = SymbolUnverified
		out.Reason = "not inside a code repository"
		return out
	}
	if r.Repo != "" && project != "" && !sameRepo(r.Repo, project) {
		out.PathStatus = PathExternal
		out.SymbolStatus = SymbolExternal
		out.Reason = fmt.Sprintf("reference repo %s is not the current repo %s", r.Repo, project)
		return out
	}
	if r.Repo != "" && project == "" {
		out.PathStatus = PathExternal
		out.SymbolStatus = SymbolExternal
		out.Reason = "current repository is unknown and the reference names another repo"
		return out
	}

	commit, err := resolveCommit(ctx, v.Root, r.Ref, r.Commit)
	if err != nil {
		out.PathStatus = PathUnverified
		out.SymbolStatus = SymbolUnverified
		out.Reason = "cannot resolve ref: " + err.Error()
		return out
	}
	out.Commit = commit
	if _, err := gitx.Run(ctx, v.Root, "cat-file", "-e", commit+":"+r.Path); err != nil {
		out.PathStatus = PathMissing
		out.SymbolStatus = SymbolUnverified
		out.Reason = fmt.Sprintf("path %s does not exist at %s", r.Path, shortCommit(commit))
		return out
	}
	out.PathStatus = PathValid
	out.Provider = "git"
	if changed, err := gitx.Run(ctx, v.Root, "log", "-1", "--format=%cs", commit, "--", r.Path); err == nil {
		out.CodeChangedAt = changed
		out.DriftStatus = driftFor(changed, lastReviewed)
		if out.DriftStatus == DriftStale {
			out.DriftReason = fmt.Sprintf("code changed on %s after last_reviewed %s", changed, lastReviewed)
		}
	}
	if r.Symbol == "" {
		return out
	}
	if isGoPath(r.Path) {
		gp := v.Go
		if gp == nil {
			gp = NewGoProvider()
		}
		sym, err := gp.Resolve(ctx, ResolveRequest{
			Repo: v.Project, Root: v.Root, Commit: commit, Path: r.Path, Symbol: r.Symbol,
		})
		if err == nil {
			out.SymbolStatus = SymbolValid
			out.Provider = gp.Name()
			out.Reason = fmt.Sprintf("resolved %s at %s:%d", sym.Qualified, sym.Path, sym.Line)
			if r.Fingerprint != "" {
				if sym.Fingerprint != r.Fingerprint {
					out.DriftStatus = DriftStale
					out.DriftReason = "symbol fingerprint changed"
				} else if out.DriftStatus != DriftStale {
					out.DriftStatus = DriftFresh
					out.DriftReason = "symbol fingerprint matches"
				}
			}
			return out
		}
		out.Provider = gp.Name()
		switch {
		case errors.Is(err, ErrSymbolNotFound):
			out.SymbolStatus = SymbolMissing
			out.Reason = "symbol not found by the Go provider"
		case errors.Is(err, ErrSymbolAmbiguous):
			out.SymbolStatus = SymbolUnverified
			out.Reason = err.Error()
		case errors.Is(err, ErrUnsupported), errors.Is(err, ErrCommitMismatch):
			out.SymbolStatus = SymbolUnverified
			out.Reason = err.Error()
		default:
			out.SymbolStatus = SymbolUnverified
			out.Reason = "Go provider failed: " + err.Error()
		}
		return out
	}
	out.SymbolStatus = SymbolUnverified
	out.Provider = "git-grep"
	if _, err := gitx.Run(ctx, v.Root, "grep", "-q", "-F", "-e", r.Symbol, commit, "--", r.Path); err == nil {
		out.Reason = fmt.Sprintf("literal symbol match at %s; no semantic provider is available", shortCommit(commit))
	} else {
		out.Reason = "symbol is unverified; no semantic provider is available"
	}
	return out
}

func referenceFromStore(r store.ReferenceImpl) Reference {
	return Reference{
		Repo: r.Repo, Path: r.Path, Symbol: r.Symbol, Kind: r.Kind,
		Ref: r.Ref, Commit: r.Commit, Fingerprint: r.Fingerprint,
	}
}

func isGoPath(p string) bool {
	ext := path.Ext(strings.TrimSpace(p))
	return strings.EqualFold(ext, ".go") || ext == ""
}

func cleanRefPath(p string) (string, bool) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "", false
	}
	clean := path.Clean(p)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func resolveCommit(ctx context.Context, root, ref, commit string) (string, error) {
	if strings.TrimSpace(commit) != "" {
		out, err := gitx.Run(ctx, root, "rev-parse", "--verify", commit+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("commit %s is not reachable", commit)
		}
		return out, nil
	}
	if strings.TrimSpace(ref) == "" {
		ref = "main"
	}
	for _, candidate := range []string{ref, "origin/" + ref} {
		if out, err := gitx.Run(ctx, root, "rev-parse", "--verify", candidate+"^{commit}"); err == nil {
			return out, nil
		}
	}
	return "", fmt.Errorf("ref %s is not reachable", ref)
}

func driftFor(codeDate, reviewed string) string {
	if strings.TrimSpace(codeDate) == "" || strings.TrimSpace(reviewed) == "" {
		return DriftUnknown
	}
	if _, err := time.Parse("2006-01-02", reviewed); err != nil {
		return DriftUnknown
	}
	if codeDate > reviewed {
		return DriftStale
	}
	return DriftFresh
}

func sameRepo(a, b string) bool {
	return strings.EqualFold(strings.Trim(strings.TrimSpace(a), "/"), strings.Trim(strings.TrimSpace(b), "/"))
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func Summarize(docs []DocValidation) Summary {
	var s Summary
	s.Documents = len(docs)
	for _, d := range docs {
		for _, r := range d.References {
			s.References++
			switch r.PathStatus {
			case PathValid:
				s.ValidPaths++
			case PathMissing:
				s.MissingPaths++
			case PathExternal:
				s.ExternalRefs++
			case PathUnverified:
				s.Unverified++
			}
			if r.Provider == "git-grep" && strings.Contains(r.Reason, "literal symbol match") {
				s.GrepMatches++
			}
			if r.SymbolStatus == SymbolUnsupported {
				s.Unsupported++
			}
			switch r.DriftStatus {
			case DriftFresh:
				s.FreshRefs++
			case DriftStale:
				s.StaleRefs++
			case DriftUnknown:
				s.UnknownDrift++
			}
		}
	}
	return s
}

// Format renders a compact human-readable validation report.
func Format(docs []DocValidation) string {
	var b strings.Builder
	for _, d := range docs {
		fmt.Fprintf(&b, "%s\n", d.Path)
		for _, r := range d.References {
			fmt.Fprintf(&b, "  %s", r.Path)
			if r.Symbol != "" {
				fmt.Fprintf(&b, " (%s)", r.Symbol)
			}
			fmt.Fprintf(&b, ": path=%s symbol=%s drift=%s", r.PathStatus, r.SymbolStatus, r.DriftStatus)
			if r.DriftReason != "" {
				fmt.Fprintf(&b, " (%s)", r.DriftReason)
			}
			if r.Reason != "" {
				fmt.Fprintf(&b, " — %s", r.Reason)
			}
			b.WriteString("\n")
		}
	}
	s := Summarize(docs)
	fmt.Fprintf(&b, "\n%d documents, %d references: %d valid paths, %d missing, %d external, %d unverified, %d stale\n",
		s.Documents, s.References, s.ValidPaths, s.MissingPaths, s.ExternalRefs, s.Unverified, s.StaleRefs)
	return b.String()
}
