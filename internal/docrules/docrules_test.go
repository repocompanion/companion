package docrules

import (
	"strings"
	"testing"

	"github.com/bolacha/docsync/internal/store"
)

func TestValidateDoc(t *testing.T) {
	base := store.Frontmatter{
		ID: "refund", Kind: KindFlow, Title: "Refund", Status: StatusApproved,
		Owners: []string{"payments"}, LastReviewed: "2026-01-01",
	}
	cases := []struct {
		name   string
		path   string
		mutate func(*store.Frontmatter)
		want   string
	}{
		{name: "valid flow", path: "projects/acme/api/flows/refund.md"},
		{name: "valid adr", path: "org/adr/2026-01-01-use-outbox.md", mutate: func(f *store.Frontmatter) {
			f.Kind = KindADR
		}},
		{name: "valid project doc", path: "org/README.md", mutate: func(f *store.Frontmatter) {
			f.Kind = KindProjectDoc
		}},
		{name: "adr in wrong directory", path: "org/use-outbox.md", mutate: func(f *store.Frontmatter) {
			f.Kind = KindADR
		}, want: "ADRs must be one dated file under adr/"},
		{name: "flow in project root", path: "projects/acme/api/refund.md", want: "flow documents must live under flows/"},
		{name: "missing owners", path: "projects/acme/api/flows/refund.md", mutate: func(f *store.Frontmatter) {
			f.Owners = nil
		}, want: "non-empty owners"},
		{name: "missing last reviewed", path: "projects/acme/api/flows/refund.md", mutate: func(f *store.Frontmatter) {
			f.LastReviewed = ""
		}, want: "needs last_reviewed"},
		{name: "invalid date", path: "projects/acme/api/flows/refund.md", mutate: func(f *store.Frontmatter) {
			f.LastReviewed = "2026/01/01"
		}, want: "last_reviewed must be YYYY-MM-DD"},
		{name: "unknown kind", path: "projects/acme/api/flows/refund.md", mutate: func(f *store.Frontmatter) {
			f.Kind = "architecture"
		}, want: "unknown frontmatter kind"},
		{name: "unknown status", path: "projects/acme/api/flows/refund.md", mutate: func(f *store.Frontmatter) {
			f.Status = "draft"
		}, want: "unknown frontmatter status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			if tc.mutate != nil {
				tc.mutate(&f)
			}
			problems := ValidateDoc(tc.path, f)
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("unexpected problems: %+v", problems)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatalf("expected a problem containing %q", tc.want)
			}
			for _, p := range problems {
				if strings.Contains(p.Message, tc.want) {
					return
				}
			}
			t.Fatalf("problems %+v do not contain %q", problems, tc.want)
		})
	}
}

func TestValidateReferences(t *testing.T) {
	valid := store.ReferenceImpl{
		Repo: "acme/api", Path: "internal/consumer.go", Symbol: "Consumer.Run",
		Kind: "method", Fingerprint: "sha256:" + strings.Repeat("a", 64),
	}
	cases := []struct {
		name string
		ref  store.ReferenceImpl
		want string
	}{
		{name: "valid", ref: valid},
		{name: "missing repo", ref: store.ReferenceImpl{Path: "x.go"}, want: "requires repo"},
		{name: "unknown kind", ref: func() store.ReferenceImpl { r := valid; r.Kind = "module"; return r }(), want: "unknown kind"},
		{name: "bad fingerprint", ref: func() store.ReferenceImpl { r := valid; r.Fingerprint = "sha1:abc"; return r }(), want: "fingerprint must be sha256"},
		{name: "symbol whitespace", ref: func() store.ReferenceImpl { r := valid; r.Symbol = "Consumer Run"; return r }(), want: "must not contain whitespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := ValidateReferences("projects/acme/api/patterns/x.md", []store.ReferenceImpl{tc.ref})
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("unexpected problems: %+v", problems)
				}
				return
			}
			for _, p := range problems {
				if strings.Contains(p.Message, tc.want) {
					return
				}
			}
			t.Fatalf("problems %+v do not contain %q", problems, tc.want)
		})
	}
}

func TestValidateAreaRequiresCanonicalDepth(t *testing.T) {
	valid := []string{"org", "teams/payments", "teams/data platform", "projects/acme/api", ".docsync", "/org/", "projects/acme/api/"}
	for _, area := range valid {
		if err := ValidateArea(area); err != nil {
			t.Errorf("ValidateArea(%q) = %v, want nil", area, err)
		}
	}
	invalid := []string{
		"", "teams", "teams/", "teams/payments/extra", "teams/..", "projects/acme",
		"projects/acme/api/extra", "org/payments", "org//payments",
		"projects/../api", "projects/acme/..", "projects/acme/api//x",
		"projects/acme/api?x", ".docsync/registry", "random",
	}
	for _, area := range invalid {
		if err := ValidateArea(area); err == nil {
			t.Errorf("ValidateArea(%q) = nil, want an error", area)
		}
	}
}

func TestValidateProject(t *testing.T) {
	for _, project := range []string{"acme/api", "acme/api-v2", "owner_1/repo.go", "local/foo"} {
		if err := ValidateProject(project); err != nil {
			t.Errorf("ValidateProject(%q) = %v, want nil", project, err)
		}
	}
	for _, project := range []string{"", "acme", "acme/api/extra", "../api", "acme/..", "acme/api?x"} {
		if err := ValidateProject(project); err == nil {
			t.Errorf("ValidateProject(%q) = nil, want an error", project)
		}
	}
}
