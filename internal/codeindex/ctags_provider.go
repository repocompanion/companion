package codeindex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/gitx"
)

const ctagsIndexVersion = "universal-ctags-v2"

type CtagsProvider struct {
	Binary      string
	ToolVersion string
	Available   bool
	CacheDir    string
}

func NewCtagsProvider() *CtagsProvider {
	p := &CtagsProvider{CacheDir: defaultCodeCacheDir()}
	for _, name := range []string{"universal-ctags", "ctags"} {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(bin, "--version").Output()
		if err != nil || !strings.Contains(string(out), "Universal Ctags") {
			continue
		}
		p.Binary = bin
		p.ToolVersion = firstLine(string(out))
		p.Available = true
		return p
	}
	return p
}

func (p *CtagsProvider) Name() string { return "ctags" }

func (p *CtagsProvider) Version() string {
	if p.ToolVersion == "" {
		return ctagsIndexVersion
	}
	return ctagsIndexVersion + "-" + p.ToolVersion
}

func (p *CtagsProvider) Languages() []string {
	return []string{"go", "typescript", "javascript", "php", "python", "java", "kotlin", "rust", "ruby", "c", "cpp", "csharp", "swift", "scala", "shell"}
}

func (p *CtagsProvider) Capabilities() Capabilities {
	return Capabilities{Index: true, Paths: true, Symbols: true, Fingerprint: true}
}

func (p *CtagsProvider) SupportsPath(file string) bool {
	switch strings.ToLower(path.Ext(strings.TrimSpace(file))) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".php", ".py", ".pyi", ".java", ".kt", ".kts", ".rs",
		".rb", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".swift", ".scala", ".sh":
		return true
	default:
		return false
	}
}

func (p *CtagsProvider) Index(ctx context.Context, req IndexRequest) (*Snapshot, error) {
	if !p.Available || p.Binary == "" {
		return nil, ErrUnsupported
	}
	head, err := gitx.Run(ctx, req.Root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if req.Commit == "" {
		req.Commit = head
	}
	if cached := p.loadCache(req); cached != nil {
		return cached, nil
	}
	root, cleanup, err := checkoutAt(ctx, req.Root, req.Commit)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	args := []string{
		"--output-format=json", "--fields=+nKSt", "--extras=+q",
		"--exclude=.git", "--exclude=node_modules", "--exclude=vendor",
		"--exclude=dist", "--exclude=build", "--exclude=.companion",
		"-R", ".",
	}
	cmd := exec.CommandContext(ctx, p.Binary, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ctags index: %w", err)
	}
	snapshot := &Snapshot{
		Repo: req.Repo, Commit: req.Commit, Provider: p.Name(), Version: p.Version(),
		IndexedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var tag ctagsTag
		if err := json.Unmarshal([]byte(line), &tag); err != nil || tag.Type != "tag" || tag.Name == "" || tag.Path == "" {
			continue
		}
		rel := tag.Path
		if filepath.IsAbs(rel) {
			if r, err := filepath.Rel(root, rel); err == nil {
				rel = r
			}
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if !p.SupportsPath(rel) {
			continue
		}
		qualified := tag.Name
		if tag.Scope != "" {
			qualified = tag.Scope + "." + tag.Name
		}
		sig := tag.Signature
		if sig == "" {
			sig = tag.Pattern
		}
		sym := Symbol{
			Provider: p.Name(), Repo: req.Repo, Commit: req.Commit, Package: tag.Scope,
			Name: tag.Name, Qualified: qualified, Kind: normalizeCtagsKind(tag.Kind),
			Path: rel, Line: tag.Line, Signature: sig,
		}
		sym.Fingerprint = symbolFingerprint(sym)
		snapshot.Symbols = append(snapshot.Symbols, sym)
	}
	if len(snapshot.Symbols) == 0 {
		return nil, ErrUnsupported
	}
	sort.Slice(snapshot.Symbols, func(i, j int) bool {
		if snapshot.Symbols[i].Path != snapshot.Symbols[j].Path {
			return snapshot.Symbols[i].Path < snapshot.Symbols[j].Path
		}
		if snapshot.Symbols[i].Line != snapshot.Symbols[j].Line {
			return snapshot.Symbols[i].Line < snapshot.Symbols[j].Line
		}
		return snapshot.Symbols[i].Qualified < snapshot.Symbols[j].Qualified
	})
	p.saveCache(req, snapshot)
	return snapshot, nil
}

func (p *CtagsProvider) Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error) {
	commit := req.Commit
	if commit == "" {
		out, err := gitx.Run(ctx, req.Root, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		commit = out
	}
	snap, err := p.Index(ctx, IndexRequest{Repo: req.Repo, Root: req.Root, Commit: commit})
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.Symbol)
	pathFilter := strings.Trim(path.Clean(strings.ReplaceAll(req.Path, "\\", "/")), "/")
	var matches []Symbol
	for _, s := range snap.Symbols {
		if pathFilter != "" && pathFilter != "." && !pathMatches(s.Path, pathFilter) {
			continue
		}
		if symbolMatches(s, query) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return nil, ErrSymbolNotFound
	}
	if len(matches) > 1 {
		var exact []Symbol
		for _, s := range matches {
			if strings.EqualFold(s.Qualified, query) || strings.EqualFold(s.Name, query) {
				exact = append(exact, s)
			}
		}
		if len(exact) == 1 {
			return &exact[0], nil
		}
		return nil, fmt.Errorf("%w: %s (%d matches)", ErrSymbolAmbiguous, query, len(matches))
	}
	return &matches[0], nil
}

type ctagsTag struct {
	Type      string `json:"_type"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	Signature string `json:"signature"`
	Pattern   string `json:"pattern"`
	Scope     string `json:"scope"`
	ScopeKind string `json:"scopeKind"`
	Language  string `json:"language"`
}

func normalizeCtagsKind(kind string) string {
	switch strings.ToLower(kind) {
	case "f", "func", "function":
		return "function"
	case "m", "method":
		return "method"
	case "c", "class", "struct", "interface", "t", "type", "typedef":
		return "type"
	default:
		return strings.ToLower(kind)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func (p *CtagsProvider) cachePath(req IndexRequest) string {
	if p.CacheDir == "" || req.Commit == "" {
		return ""
	}
	repo := gitx.Slug(req.Repo)
	if repo == "" {
		repo = "local"
	}
	return filepath.Join(p.CacheDir, repo, req.Commit, "ctags-"+gitx.Slug(p.Version())+".json")
}

func (p *CtagsProvider) loadCache(req IndexRequest) *Snapshot {
	name := p.cachePath(req)
	if name == "" {
		return nil
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil || snap.Provider != p.Name() || snap.Version != p.Version() || snap.Commit != req.Commit {
		return nil
	}
	return &snap
}

func (p *CtagsProvider) saveCache(req IndexRequest, snap *Snapshot) {
	name := p.cachePath(req)
	if name == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, name)
}
