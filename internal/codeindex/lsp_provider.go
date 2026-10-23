package codeindex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bolacha/docsync/internal/gitx"
)

type lspServerSpec struct {
	name     string
	language string
	exts     []string
	commands [][]string
}

var lspServerSpecs = []lspServerSpec{
	{name: "go", language: "go", exts: []string{".go"}, commands: [][]string{{"gopls"}}},
	{name: "typescript", language: "typescript", exts: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}, commands: [][]string{{"typescript-language-server", "--stdio"}}},
	{name: "python", language: "python", exts: []string{".py", ".pyi"}, commands: [][]string{{"pyright-langserver", "--stdio"}, {"pylsp"}}},
	{name: "php", language: "php", exts: []string{".php"}, commands: [][]string{{"intelephense", "--stdio"}, {"phpactor", "language-server"}}},
	{name: "java", language: "java", exts: []string{".java"}, commands: [][]string{{"jdtls"}}},
	{name: "kotlin", language: "kotlin", exts: []string{".kt", ".kts"}, commands: [][]string{{"kotlin-language-server"}}},
	{name: "rust", language: "rust", exts: []string{".rs"}, commands: [][]string{{"rust-analyzer"}}},
}

type lspServer struct {
	Name     string
	Language string
	Binary   string
	Args     []string
	Exts     map[string]bool
}

type LSPProvider struct {
	Servers   []lspServer
	Available bool
}

func NewLSPProvider() *LSPProvider {
	p := &LSPProvider{}
	for _, spec := range lspServerSpecs {
		for _, command := range spec.commands {
			if len(command) == 0 {
				continue
			}
			bin, err := exec.LookPath(command[0])
			if err != nil {
				continue
			}
			exts := map[string]bool{}
			for _, ext := range spec.exts {
				exts[ext] = true
			}
			p.Servers = append(p.Servers, lspServer{
				Name: spec.name, Language: spec.language, Binary: bin,
				Args: command[1:], Exts: exts,
			})
			break
		}
	}
	p.Available = len(p.Servers) > 0
	return p
}

func (p *LSPProvider) Name() string { return "lsp" }

func (p *LSPProvider) Version() string { return "lsp-v1" }

func (p *LSPProvider) Languages() []string {
	var out []string
	for _, server := range p.Servers {
		out = append(out, server.Language)
	}
	return out
}

func (p *LSPProvider) Capabilities() Capabilities {
	return Capabilities{Symbols: true, Definitions: true, References: true}
}

func (p *LSPProvider) SupportsPath(file string) bool {
	return p.serverForPath(file) != nil
}

// Index is intentionally unsupported: LSP is a live, current-repo resolver,
// not a durable snapshot source.
func (p *LSPProvider) Index(context.Context, IndexRequest) (*Snapshot, error) {
	return nil, ErrUnsupported
}

func (p *LSPProvider) Resolve(ctx context.Context, req ResolveRequest) (*Symbol, error) {
	server := p.serverForPath(req.Path)
	if server == nil {
		return nil, ErrUnsupported
	}
	if strings.TrimSpace(req.Root) == "" || strings.TrimSpace(req.Path) == "" {
		return nil, ErrUnsupported
	}
	head, err := gitx.Run(ctx, req.Root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if req.Commit != "" && req.Commit != head {
		return nil, ErrCommitMismatch
	}
	file := filepath.Join(req.Root, filepath.FromSlash(req.Path))
	pos, err := findSymbolPosition(file, req.Symbol)
	if err != nil {
		return nil, err
	}
	client, err := startLSPClient(ctx, req.Root, *server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	loc, err := client.definition(ctx, file, pos)
	if err != nil {
		return nil, err
	}
	if loc == nil {
		return nil, ErrSymbolNotFound
	}
	rel := loc.Path
	if r, err := filepath.Rel(req.Root, loc.Path); err == nil {
		rel = filepath.ToSlash(r)
	}
	return &Symbol{
		Provider: "lsp-" + server.Name, Repo: req.Repo, Commit: head,
		Package: server.Language, Name: lastSymbolPart(req.Symbol), Qualified: req.Symbol,
		Kind: "symbol", Path: rel, Line: loc.Line + 1, Signature: loc.Signature,
	}, nil
}

func (p *LSPProvider) References(ctx context.Context, req ResolveRequest, includeDeclaration bool) ([]ReferenceLocation, error) {
	server := p.serverForPath(req.Path)
	if server == nil || strings.TrimSpace(req.Root) == "" || strings.TrimSpace(req.Path) == "" {
		return nil, ErrUnsupported
	}
	head, err := gitx.Run(ctx, req.Root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if req.Commit != "" && req.Commit != head {
		return nil, ErrCommitMismatch
	}
	file := filepath.Join(req.Root, filepath.FromSlash(req.Path))
	pos, err := findSymbolPosition(file, req.Symbol)
	if err != nil {
		return nil, err
	}
	client, err := startLSPClient(ctx, req.Root, *server)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	locs, err := client.references(ctx, file, pos, includeDeclaration)
	if err != nil {
		return nil, err
	}
	out := make([]ReferenceLocation, 0, len(locs))
	for _, loc := range locs {
		rel := loc.Path
		if r, err := filepath.Rel(req.Root, loc.Path); err == nil {
			rel = filepath.ToSlash(r)
		}
		out = append(out, ReferenceLocation{Path: rel, Line: loc.Line + 1, Signature: loc.Signature})
	}
	return out, nil
}

func (p *LSPProvider) serverForPath(file string) *lspServer {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(file)))
	for i := range p.Servers {
		if p.Servers[i].Exts[ext] {
			return &p.Servers[i]
		}
	}
	return nil
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspLocation struct {
	Path      string
	Line      int
	Signature string
}

func findSymbolPosition(file, symbol string) (lspPosition, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return lspPosition{}, err
	}
	candidates := []string{symbol}
	if i := strings.LastIndexAny(symbol, ".#:"); i >= 0 && i+1 < len(symbol) {
		candidates = append(candidates, symbol[i+1:])
	}
	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if j := strings.Index(line, cand); j >= 0 {
				return lspPosition{Line: i, Character: j}, nil
			}
		}
	}
	return lspPosition{}, ErrSymbolNotFound
}

func lastSymbolPart(symbol string) string {
	if i := strings.LastIndexAny(symbol, ".#:"); i >= 0 && i+1 < len(symbol) {
		return symbol[i+1:]
	}
	return symbol
}

type lspClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID int
}

func startLSPClient(ctx context.Context, root string, server lspServer) (*lspClient, error) {
	cmd := exec.CommandContext(ctx, server.Binary, server.Args...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &lspClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), nextID: 1}
	if err := c.initialize(ctx, root); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	return c, nil
}

func (c *lspClient) initialize(ctx context.Context, root string) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   pathToFileURI(root),
		"rootPath":  root,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition": map[string]any{"linkSupport": true},
				"references": map[string]any{},
			},
			"workspace": map[string]any{"workspaceFolders": true},
		},
		"workspaceFolders": []map[string]any{{"uri": pathToFileURI(root), "name": filepath.Base(root)}},
	}
	if _, err := c.request(ctx, "initialize", params); err != nil {
		return err
	}
	return c.notify(ctx, "initialized", map[string]any{})
}

func (c *lspClient) definition(ctx context.Context, file string, pos lspPosition) (*lspLocation, error) {
	raw, err := c.request(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": pathToFileURI(file)},
		"position":     map[string]any{"line": pos.Line, "character": pos.Character},
	})
	if err != nil {
		return nil, err
	}
	locations, err := parseLSPLocations(raw)
	if err != nil {
		return nil, err
	}
	if len(locations) == 0 {
		return nil, nil
	}
	loc := locations[0]
	if loc.Path == "" {
		return nil, ErrSymbolNotFound
	}
	return &loc, nil
}

func (c *lspClient) references(ctx context.Context, file string, pos lspPosition, includeDeclaration bool) ([]lspLocation, error) {
	raw, err := c.request(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": pathToFileURI(file)},
		"position":     map[string]any{"line": pos.Line, "character": pos.Character},
		"context":      map[string]any{"includeDeclaration": includeDeclaration},
	})
	if err != nil {
		return nil, err
	}
	return parseLSPLocations(raw)
}

func (c *lspClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	if err := c.write(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		body, err := c.read(ctx)
		if err != nil {
			return nil, err
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			result := any(nil)
			if msg.Method == "workspace/configuration" {
				result = []any{}
			}
			_ = c.write(ctx, map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
			continue
		}
		if len(msg.ID) == 0 || msg.Method != "" {
			continue
		}
		var gotID int
		if err := json.Unmarshal(msg.ID, &gotID); err != nil || gotID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("lsp %s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (c *lspClient) notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.write(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *lspClient) write(ctx context.Context, msg any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (c *lspClient) read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	length := 0
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
			if err != nil {
				return nil, fmt.Errorf("invalid LSP Content-Length: %w", err)
			}
			length = n
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("missing LSP Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *lspClient) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.request(ctx, "shutdown", nil)
	_ = c.notify(ctx, "exit", nil)
	_ = c.stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
	}
}

type lspRawLocation struct {
	URI       string `json:"uri"`
	TargetURI string `json:"targetUri"`
	Range     struct {
		Start lspPosition `json:"start"`
	} `json:"range"`
	TargetRange struct {
		Start lspPosition `json:"start"`
	} `json:"targetRange"`
}

func parseLSPLocations(raw json.RawMessage) ([]lspLocation, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []lspRawLocation
	if err := json.Unmarshal(raw, &items); err != nil {
		var one lspRawLocation
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, err
		}
		items = []lspRawLocation{one}
	}
	var out []lspLocation
	for _, item := range items {
		uri := item.URI
		if uri == "" {
			uri = item.TargetURI
		}
		start := item.Range.Start
		if uri == "" {
			start = item.TargetRange.Start
		}
		file, err := uriToPath(uri)
		if err != nil || file == "" {
			continue
		}
		line := start.Line
		sig := ""
		if b, err := os.ReadFile(file); err == nil {
			lines := strings.Split(string(b), "\n")
			if line >= 0 && line < len(lines) {
				sig = strings.TrimSpace(lines[line])
			}
		}
		out = append(out, lspLocation{Path: file, Line: line, Signature: sig})
	}
	return out, nil
}

func pathToFileURI(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

func uriToPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		return "", fmt.Errorf("not a file URI")
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}
