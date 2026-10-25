package mcpserver

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/repocompanion/companion/internal/gitx"
)

// rootFor returns the first usable MCP root for a session. Roots are
// deprecated by the MCP specification but remain supported as a fallback
// discovery mechanism; explicit project/config values still win.
func (p *provider) rootFor(ctx context.Context, session *mcp.ServerSession) string {
	if session == nil {
		return ""
	}
	p.rootsMu.Lock()
	if p.rootsKnown[session] {
		root := p.roots[session]
		p.rootsMu.Unlock()
		return root
	}
	p.rootsMu.Unlock()

	root := discoverRoot(ctx, session)
	p.rootsMu.Lock()
	p.roots[session] = root
	p.rootsKnown[session] = true
	p.rootsMu.Unlock()
	return root
}

func (p *provider) invalidateRoot(session *mcp.ServerSession) {
	if session == nil {
		return
	}
	p.rootsMu.Lock()
	delete(p.roots, session)
	delete(p.rootsKnown, session)
	p.rootsMu.Unlock()
}

func discoverRoot(ctx context.Context, session *mcp.ServerSession) string {
	// Asking for roots when the client did not advertise the capability can
	// block the first tool call indefinitely: clients are allowed to ignore
	// unknown server-initiated requests. Check the capability first, and keep a
	// short deadline for clients that advertise roots but do not answer.
	if !clientSupportsRoots(session) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := session.ListRoots(ctx, nil)
	if err != nil || res == nil {
		return ""
	}
	var fallback string
	for _, root := range res.Roots {
		p := fileRootPath(root.URI)
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			p = filepath.Dir(p)
		}
		if fallback == "" {
			fallback = p
		}
		if detected := gitx.Detect(ctx, p); detected.Root != "" {
			return detected.Root
		}
	}
	return fallback
}

func clientSupportsRoots(session *mcp.ServerSession) bool {
	if session == nil {
		return false
	}
	params := session.InitializeParams()
	return params != nil && params.Capabilities != nil && params.Capabilities.RootsV2 != nil
}

func fileRootPath(uri string) string {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return ""
	}
	if u.Host != "" && u.Host != "localhost" {
		p = "//" + u.Host + p
	}
	if os.PathSeparator == '\\' && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}
