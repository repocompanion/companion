package codeindex

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

const scipIndexVersion = "scip-v1"

type SCIPProvider struct {
	IndexFile string
}

func NewSCIPProvider() *SCIPProvider {
	p := &SCIPProvider{IndexFile: strings.TrimSpace(os.Getenv("DOCSYNC_SCIP_INDEX"))}
	if p.IndexFile == "" {
		if wd, err := os.Getwd(); err == nil {
			for _, name := range []string{"index.scip", filepath.Join(".docsync", "code", "index.scip")} {
				candidate := filepath.Join(wd, name)
				if _, err := os.Stat(candidate); err == nil {
					p.IndexFile = candidate
					break
				}
			}
		}
	}
	return p
}

func (p *SCIPProvider) Name() string { return "scip" }

func (p *SCIPProvider) Version() string { return scipIndexVersion }

func (p *SCIPProvider) Languages() []string { return []string{"scip"} }

func (p *SCIPProvider) Capabilities() Capabilities {
	return Capabilities{Index: true, Paths: true, Symbols: true, Definitions: true, References: true, Fingerprint: true}
}

func (p *SCIPProvider) Available() bool {
	if p == nil || strings.TrimSpace(p.IndexFile) == "" {
		return false
	}
	_, err := os.Stat(p.IndexFile)
	return err == nil
}

func (p *SCIPProvider) SupportsPath(file string) bool {
	return strings.TrimSpace(file) != "" && !strings.EqualFold(path.Ext(file), ".md")
}

func (p *SCIPProvider) Index(ctx context.Context, req IndexRequest) (*Snapshot, error) {
	idx, err := p.load(req.Root)
	if err != nil {
		return nil, err
	}
	snapshot := &Snapshot{Repo: req.Repo, Commit: req.Commit, Provider: p.Name(), Version: p.Version()}
	for _, doc := range idx.Documents {
		if doc == nil {
			continue
		}
		infos := map[string]*scip.SymbolInformation{}
		for _, info := range doc.Symbols {
			if info != nil {
				infos[info.Symbol] = info
			}
		}
		for _, occ := range doc.Occurrences {
			if occ == nil || occ.Symbol == "" || !scip.SymbolRole_Definition.Matches(occ) {
				continue
			}
			info := infos[occ.Symbol]
			display := scipDisplayName(occ.Symbol, info)
			sig := scipSignature(info)
			if sig == "" {
				sig = sourceLine(req.Root, doc.RelativePath, occurrenceLine(occ))
			}
			sym := Symbol{
				Provider: p.Name(), SCIP: occ.Symbol, Repo: req.Repo, Commit: req.Commit,
				Package: doc.Language, Name: display, Qualified: display,
				Kind: scipKind(info), Path: doc.RelativePath, Line: occurrenceLine(occ) + 1,
				Signature: sig,
			}
			sym.Fingerprint = symbolFingerprint(sym)
			snapshot.Symbols = append(snapshot.Symbols, sym)
		}
	}
	if len(snapshot.Symbols) == 0 {
		return nil, ErrUnsupported
	}
	sortSymbols(snapshot.Symbols)
	return snapshot, nil
}

func (p *SCIPProvider) Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error) {
	snap, err := p.Index(ctx, IndexRequest{Repo: req.Repo, Root: req.Root, Commit: req.Commit})
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
		if symbolMatches(s, query) || strings.Contains(s.SCIP, query) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return nil, ErrSymbolNotFound
	}
	if len(matches) > 1 {
		var exact []Symbol
		for _, s := range matches {
			if strings.EqualFold(s.Qualified, query) || strings.EqualFold(s.Name, query) || s.SCIP == query {
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

func (p *SCIPProvider) References(ctx context.Context, req ResolveRequest, includeDeclaration bool) ([]ReferenceLocation, error) {
	sym, err := p.Resolve(ctx, req)
	if err != nil {
		return nil, err
	}
	idx, err := p.load(req.Root)
	if err != nil {
		return nil, err
	}
	var out []ReferenceLocation
	for _, doc := range idx.Documents {
		if doc == nil {
			continue
		}
		for _, occ := range doc.Occurrences {
			if occ == nil || occ.Symbol != sym.SCIP {
				continue
			}
			isDef := scip.SymbolRole_Definition.Matches(occ)
			if isDef && !includeDeclaration {
				continue
			}
			out = append(out, ReferenceLocation{
				Path: doc.RelativePath, Line: occurrenceLine(occ) + 1,
				Signature: sourceLine(req.Root, doc.RelativePath, occurrenceLine(occ)),
			})
		}
	}
	return out, nil
}

func (p *SCIPProvider) load(root string) (*scip.Index, error) {
	name := p.indexPath(root)
	if name == "" {
		return nil, ErrUnsupported
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read SCIP index: %w", err)
	}
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		zr, err := gzip.NewReader(strings.NewReader(string(b)))
		if err != nil {
			return nil, fmt.Errorf("open gzip SCIP index: %w", err)
		}
		defer zr.Close()
		if b, err = io.ReadAll(zr); err != nil {
			return nil, fmt.Errorf("read gzip SCIP index: %w", err)
		}
	}
	var idx scip.Index
	if err := proto.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("parse SCIP index: %w", err)
	}
	return &idx, nil
}

func (p *SCIPProvider) indexPath(root string) string {
	if p.IndexFile != "" {
		if filepath.IsAbs(p.IndexFile) {
			return p.IndexFile
		}
		return filepath.Join(root, p.IndexFile)
	}
	for _, name := range []string{"index.scip", filepath.Join(".docsync", "code", "index.scip")} {
		candidate := filepath.Join(root, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func scipDisplayName(raw string, info *scip.SymbolInformation) string {
	if info != nil && strings.TrimSpace(info.DisplayName) != "" {
		return strings.TrimSpace(info.DisplayName)
	}
	raw = strings.TrimSuffix(raw, ".")
	if i := strings.LastIndexAny(raw, "/#."); i >= 0 && i+1 < len(raw) {
		return raw[i+1:]
	}
	return raw
}

func scipSignature(info *scip.SymbolInformation) string {
	if info == nil || info.SignatureDocumentation == nil {
		return ""
	}
	return strings.TrimSpace(info.SignatureDocumentation.Text)
}

func scipKind(info *scip.SymbolInformation) string {
	if info == nil {
		return "symbol"
	}
	switch info.Kind {
	case scip.SymbolInformation_Function, scip.SymbolInformation_AbstractMethod:
		return "function"
	case scip.SymbolInformation_Method, scip.SymbolInformation_MethodSpecification:
		return "method"
	case scip.SymbolInformation_Class, scip.SymbolInformation_Interface, scip.SymbolInformation_Struct, scip.SymbolInformation_Type, scip.SymbolInformation_Enum:
		return "type"
	case scip.SymbolInformation_Package, scip.SymbolInformation_Module, scip.SymbolInformation_Namespace:
		return "package"
	default:
		return "symbol"
	}
}

func occurrenceLine(occ *scip.Occurrence) int {
	if occ == nil || len(occ.Range) == 0 {
		return 0
	}
	return int(occ.Range[0])
}

func sourceLine(root, rel string, line int) string {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line])
}

func sortSymbols(symbols []Symbol) {
	for i := 1; i < len(symbols); i++ {
		for j := i; j > 0; j-- {
			a, b := symbols[j-1], symbols[j]
			if a.Path < b.Path || (a.Path == b.Path && (a.Line < b.Line || (a.Line == b.Line && a.Qualified <= b.Qualified))) {
				break
			}
			symbols[j-1], symbols[j] = symbols[j], symbols[j-1]
		}
	}
}
