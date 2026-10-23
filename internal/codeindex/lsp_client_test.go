package codeindex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
)

func TestLSPClientDefinition(t *testing.T) {
	root := testRepo(t)
	file := filepath.Join(root, "app.ts")
	if err := os.WriteFile(file, []byte("export function handler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(context.Background(), root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "add ts"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WANT_LSP_HELPER", "1")
	t.Setenv("LSP_FAKE_FILE", file)
	p := &LSPProvider{
		Servers: []lspServer{{Name: "fake", Language: "typescript", Binary: os.Args[0],
			Args: []string{"-test.run=TestLSPHelperProcess"}, Exts: map[string]bool{".ts": true}}},
		Available: true,
	}
	commit, err := gitx.Run(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sym, err := p.Resolve(context.Background(), ResolveRequest{Repo: "acme/api", Root: root, Commit: commit, Path: "app.ts", Symbol: "handler"})
	if err != nil {
		t.Fatal(err)
	}
	if sym.Provider != "lsp-fake" || sym.Path != "app.ts" || sym.Line != 1 || !strings.Contains(sym.Signature, "handler") {
		t.Fatalf("symbol = %+v", sym)
	}
}

func TestLSPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LSP_HELPER") != "1" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	for {
		body, err := readTestFrame(r)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(0)
		}
		var msg map[string]any
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		id := msg["id"]
		method, _ := msg["method"].(string)
		switch method {
		case "initialize":
			writeTestFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"capabilities": map[string]any{}}})
		case "initialized":
		case "textDocument/definition":
			target := pathToFileURI(os.Getenv("LSP_FAKE_FILE"))
			writeTestFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": []any{map[string]any{
				"uri": target, "range": map[string]any{"start": map[string]any{"line": 0, "character": 0}},
			}}})
		case "shutdown":
			writeTestFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
		case "exit":
			os.Exit(0)
		default:
			if id != nil {
				writeTestFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": nil})
			}
		}
	}
}

func readTestFrame(r *bufio.Reader) ([]byte, error) {
	n := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, _ = strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
		}
	}
	if n <= 0 {
		return nil, fmt.Errorf("missing content length")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(r, body)
	return body, err
}

func writeTestFrame(msg any) {
	b, _ := json.Marshal(msg)
	fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
}
