package codeindex

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/store"
)

type Validator struct {
	Root    string
	Project string
}

// ValidateDocs validates every reference on the supplied documents.
func ValidateDocs(ctx context.Context, root, project string, docs []*store.Doc) ([]DocValidation, error) {
	v := Validator{Root: root, Project: project}
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
		rv := v.validateRef(ctx, referenceFromStore(r))
		out.References = append(out.References, rv)
	}
	return out, nil
}

func (v Validator) validateRef(ctx context.Context, r Reference) ReferenceValidation {
	out := ReferenceValidation{Reference: r, SymbolStatus: SymbolNotRequested}
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
	if r.Symbol == "" {
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
			fmt.Fprintf(&b, ": path=%s symbol=%s", r.PathStatus, r.SymbolStatus)
			if r.Reason != "" {
				fmt.Fprintf(&b, " — %s", r.Reason)
			}
			b.WriteString("\n")
		}
	}
	s := Summarize(docs)
	fmt.Fprintf(&b, "\n%d documents, %d references: %d valid paths, %d missing, %d external, %d unverified\n",
		s.Documents, s.References, s.ValidPaths, s.MissingPaths, s.ExternalRefs, s.Unverified)
	return b.String()
}
