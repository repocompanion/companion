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
