package codeindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/gitx"
	"golang.org/x/tools/go/packages"
)

const (
	goIndexVersion = "go-packages-v2"
	goAllPackages  = "./..."
)

type GoProvider struct {
	CacheDir string
}

func NewGoProvider() *GoProvider {
	return &GoProvider{CacheDir: defaultCodeCacheDir()}
}

func (p *GoProvider) Name() string { return "go" }

func (p *GoProvider) Version() string {
	return goIndexVersion + "-" + runtime.Version()
}

func (p *GoProvider) Languages() []string { return []string{"go"} }

func (p *GoProvider) SupportsPath(file string) bool {
	ext := strings.ToLower(path.Ext(strings.TrimSpace(file)))
	return ext == ".go" || ext == ""
}

func (p *GoProvider) Capabilities() Capabilities {
	return Capabilities{Index: true, Paths: true, Symbols: true, Definitions: true, Fingerprint: true}
}

func (p *GoProvider) Index(ctx context.Context, req IndexRequest) (*Snapshot, error) {
	if strings.TrimSpace(req.Root) == "" {
		return nil, fmt.Errorf("go index: root is required")
	}
	head, err := gitx.Run(ctx, req.Root, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("go index: resolve HEAD: %w", err)
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

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedModule,
		Dir:     root,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, goAllPackages)
	if err != nil {
		return nil, fmt.Errorf("go index: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, ErrUnsupported
	}
	snapshot := &Snapshot{
		Repo: req.Repo, Commit: req.Commit, Provider: p.Name(), Version: p.Version(),
		IndexedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, pkg := range pkgs {
		if pkg == nil || pkg.Name == "" {
			continue
		}
		snapshot.Symbols = append(snapshot.Symbols, indexPackage(root, req.Repo, req.Commit, pkg)...)
	}
	snapshot.Symbols = append(snapshot.Symbols, indexTestFiles(ctx, root, req.Repo, req.Commit)...)
	snapshot.Symbols = append(snapshot.Symbols, indexMigrationFiles(ctx, root, req.Repo, req.Commit)...)
	snapshot.Symbols = dedupeSymbols(snapshot.Symbols)
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

func (p *GoProvider) Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error) {
	commit := req.Commit
	if commit == "" {
		out, err := gitx.Run(ctx, req.Root, "rev-parse", "HEAD")
		if err != nil {
			return nil, err
		}
		commit = out
	}
	snapshot, err := p.Index(ctx, IndexRequest{Repo: req.Repo, Root: req.Root, Commit: commit})
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.Symbol)
	if query == "" {
		return nil, ErrSymbolNotFound
	}
	pathFilter := strings.Trim(path.Clean(strings.ReplaceAll(req.Path, "\\", "/")), "/")
	var matches []Symbol
	for _, s := range snapshot.Symbols {
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
		// Prefer an exact qualified/name match, then a path-relative match.
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

func pathMatches(symbolPath, filter string) bool {
	if symbolPath == filter {
		return true
	}
	return strings.HasPrefix(symbolPath, strings.TrimSuffix(filter, "/")+"/")
}

func symbolMatches(s Symbol, query string) bool {
	if strings.EqualFold(s.Qualified, query) || strings.EqualFold(s.Name, query) {
		return true
	}
	return strings.HasSuffix(s.Qualified, "."+query)
}

func indexPackage(root, repo, commit string, pkg *packages.Package) []Symbol {
	var out []Symbol
	if pkg.Fset == nil {
		return out
	}
	for _, file := range pkg.Syntax {
		filename := pkg.Fset.Position(file.Pos()).Filename
		rel, err := filepath.Rel(root, filename)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") {
			// Test files are indexed separately so they work even when the
			// package cannot be loaded with all test dependencies.
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				name := node.Name.Name
				qualified := pkg.Name + "." + name
				kind := "function"
				if recv := receiverName(node.Recv); recv != "" {
					qualified = pkg.Name + "." + recv + "." + name
					kind = "method"
				} else if isJobFunc(name, rel, pkg.Name) {
					kind = "job"
				}
				out = append(out, Symbol{
					Provider: "go", Repo: repo, Commit: commit, Package: pkg.Name, Name: name,
					Qualified: qualified, Kind: kind, Path: rel,
					Line:      pkg.Fset.Position(node.Pos()).Line,
					Signature: signature(pkg.Fset, node.Type),
				})
			case *ast.TypeSpec:
				out = append(out, Symbol{
					Provider: "go", Repo: repo, Commit: commit, Package: pkg.Name, Name: node.Name.Name,
					Qualified: pkg.Name + "." + node.Name.Name, Kind: "type", Path: rel,
					Line:      pkg.Fset.Position(node.Pos()).Line,
					Signature: "type " + node.Name.Name,
				})
				out = append(out, configFieldSymbols(pkg.Fset, pkg.Name, repo, commit, rel, node)...)
			case *ast.CallExpr:
				if route, ok := routeName(node); ok {
					out = append(out, Symbol{
						Provider: "go", Repo: repo, Commit: commit, Package: pkg.Name, Name: route,
						Qualified: pkg.Name + ".route:" + route, Kind: "route", Path: rel,
						Line: pkg.Fset.Position(node.Pos()).Line, Signature: route,
					})
				}
			}
			return true
		})
	}
	for i := range out {
		out[i].Fingerprint = symbolFingerprint(out[i])
	}
	return out
}

func isJobFunc(name, rel, pkgName string) bool {
	if !isJobPath(rel, pkgName) {
		return false
	}
	for _, prefix := range []string{"Handle", "Consume", "Process", "Run", "Start", "Listen", "Poll", "Dispatch"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isJobPath(rel, pkgName string) bool {
	needles := []string{"worker", "job", "queue", "consumer", "handler", "subscriber", "scheduler"}
	pkg := strings.ToLower(pkgName)
	for _, n := range needles {
		if strings.Contains(pkg, n) {
			return true
		}
	}
	pathLower := strings.ToLower("/" + filepath.ToSlash(rel) + "/")
	for _, n := range needles {
		if strings.Contains(pathLower, "/"+n) {
			return true
		}
	}
	return false
}

func configFieldSymbols(fset *token.FileSet, pkgName, repo, commit, rel string, typeSpec *ast.TypeSpec) []Symbol {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok || structType.Fields == nil {
		return nil
	}
	var out []Symbol
	for _, field := range structType.Fields.List {
		if field.Tag == nil || len(field.Names) == 0 {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		summary := configTagSummary(tag)
		if summary == "" {
			continue
		}
		for _, name := range field.Names {
			out = append(out, Symbol{
				Provider: "go", Repo: repo, Commit: commit, Package: pkgName, Name: name.Name,
				Qualified: pkgName + "." + typeSpec.Name.Name + "." + name.Name, Kind: "config",
				Path: rel, Line: fset.Position(name.Pos()).Line, Signature: summary,
			})
		}
	}
	return out
}

func configTagSummary(tag reflect.StructTag) string {
	var parts []string
	for _, key := range []string{"yaml", "json", "env", "mapstructure", "toml"} {
		value, ok := tag.Lookup(key)
		if !ok || value == "" || value == "-" {
			continue
		}
		parts = append(parts, key+":"+value)
	}
	return strings.Join(parts, " ")
}

func indexTestFiles(ctx context.Context, root, repo, commit string) []Symbol {
	files, err := gitFiles(ctx, root, "*_test.go")
	if err != nil {
		return nil
	}
	var out []Symbol
	fset := token.NewFileSet()
	for _, rel := range files {
		file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
		if err != nil {
			continue
		}
		pkgName := file.Name.Name
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !isTestName(fn.Name.Name) {
				continue
			}
			out = append(out, Symbol{
				Provider: "go", Repo: repo, Commit: commit, Package: pkgName,
				Name: fn.Name.Name, Qualified: pkgName + "." + fn.Name.Name, Kind: "test",
				Path: rel, Line: fset.Position(fn.Pos()).Line, Signature: signature(fset, fn.Type),
			})
		}
	}
	return out
}

func isTestName(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func indexMigrationFiles(ctx context.Context, root, repo, commit string) []Symbol {
	files, err := gitFiles(ctx, root, "*.sql", "*.go")
	if err != nil {
		return nil
	}
	var out []Symbol
	for _, rel := range files {
		if !isMigrationPath(rel) {
			continue
		}
		name := migrationName(rel)
		if name == "" {
			continue
		}
		out = append(out, Symbol{
			Provider: "go", Repo: repo, Commit: commit, Package: "migration",
			Name: name, Qualified: "migration." + name, Kind: "migration",
			Path: rel, Line: 1, Signature: "migration " + name,
		})
	}
	return out
}

func gitFiles(ctx context.Context, root string, patterns ...string) ([]string, error) {
	args := []string{"ls-files", "-z", "--"}
	args = append(args, patterns...)
	out, err := gitx.Run(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		files = append(files, filepath.ToSlash(rel))
	}
	return files, nil
}

func isMigrationPath(rel string) bool {
	lower := strings.ToLower("/" + filepath.ToSlash(rel) + "/")
	for _, marker := range []string{"/migrations/", "/migration/", "/db/migrate/", "/database/migrations/", "/sql/migrations/", "/schema/migrations/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func migrationName(rel string) string {
	base := path.Base(rel)
	ext := path.Ext(base)
	base = strings.TrimSuffix(base, ext)
	base = strings.TrimSuffix(base, ".up")
	base = strings.TrimSuffix(base, ".down")
	return base
}

func dedupeSymbols(in []Symbol) []Symbol {
	seen := map[string]bool{}
	out := make([]Symbol, 0, len(in))
	for _, s := range in {
		key := s.Provider + "\x00" + s.Path + "\x00" + s.Qualified + "\x00" + s.Kind
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func receiverName(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	return exprName(fields.List[0].Type)
}

func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return exprName(e.X)
	case *ast.IndexExpr:
		return exprName(e.X)
	case *ast.IndexListExpr:
		return exprName(e.X)
	default:
		return ""
	}
}

func routeName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") || len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, "`\\\""), true
}

func signature(fset *token.FileSet, typ ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, typ); err != nil {
		return ""
	}
	return b.String()
}

func symbolFingerprint(s Symbol) string {
	sum := sha256.Sum256([]byte(s.Kind + "|" + s.Qualified + "|" + s.Signature))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func defaultCodeCacheDir() string {
	if home := strings.TrimSpace(os.Getenv("DOCSYNC_HOME")); home != "" {
		return filepath.Join(home, "cache", "code")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".docsync", "cache", "code")
}

func (p *GoProvider) cachePath(req IndexRequest) string {
	if p.CacheDir == "" || req.Repo == "" || req.Commit == "" {
		return ""
	}
	return filepath.Join(p.CacheDir, gitx.Slug(req.Repo), req.Commit, p.Version()+".json")
}

func (p *GoProvider) loadCache(req IndexRequest) *Snapshot {
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

func (p *GoProvider) saveCache(req IndexRequest, snap *Snapshot) {
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
