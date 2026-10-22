package mcpserver

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/templates"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	docResourcePrefix      = "docsync://docs/"
	templateResourcePrefix = "docsync://templates/"
)

func registerResources(s *mcp.Server, p *provider) {
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "docsync://docs/{+path}",
		Name:        "docsync-docs",
		Title:       "docsync documents",
		Description: "Read a document from the docsync store; path is project-relative or store-relative.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		if !strings.HasPrefix(uri, docResourcePrefix) {
			return nil, fmt.Errorf("unsupported document resource %q", uri)
		}
		raw := strings.TrimPrefix(uri, docResourcePrefix)
		relPath, err := url.PathUnescape(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid document resource path: %w", err)
		}
		sess, err := p.sessionFor(ctx, req.Session, "")
		if err != nil {
			return nil, err
		}
		if err := requireStore(sess); err != nil {
			return nil, err
		}
		rel, err := resolvePath(sess, relPath)
		if err != nil {
			return nil, err
		}
		area, err := changeset.AreaForPath(rel)
		if err != nil {
			return nil, err
		}
		content, source, state, err := sess.Manager.ReadFileState(ctx, area, rel)
		if err != nil {
			return nil, err
		}
		if state == changeset.DocStateDeleted {
			return nil, fmt.Errorf("%s is deleted in %s", rel, source)
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: uri, MIMEType: "text/markdown", Text: content,
		}}}, nil
	})

	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "docsync://templates/{kind}",
		Name:        "docsync-templates",
		Title:       "docsync templates",
		Description: "Read a canonical document skeleton: adr, pattern, flow, runbook, standard, readme or architecture.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		if !strings.HasPrefix(uri, templateResourcePrefix) {
			return nil, fmt.Errorf("unsupported template resource %q", uri)
		}
		kind := strings.TrimPrefix(uri, templateResourcePrefix)
		content, err := templates.Render(kind, templates.Data{
			Title: "Document title", ID: "document-id",
			Date: time.Now().Format("2006-01-02"), Status: "proposed", Owner: "team",
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: uri, MIMEType: "text/markdown", Text: content,
		}}}, nil
	})
}
