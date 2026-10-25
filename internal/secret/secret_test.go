package secret

import (
	"strings"
	"testing"
)

func TestLooksLikeDetectsCommonCredentials(t *testing.T) {
	cases := []string{
		"AWS_SECRET_ACCESS_KEY: abcdefghijklmnopqrstuvwxyz",
		`api_key = "0123456789abcdef"`,
		"AKIAIOSFODNN7EXAMPLE",
		"ghp_" + strings.Repeat("a", 30),
		"gho_" + strings.Repeat("b", 30),
		"github_pat_" + strings.Repeat("c", 30),
		"glpat-" + strings.Repeat("d", 25),
		"sk-" + strings.Repeat("e", 25),
		"sk_live_" + strings.Repeat("f", 25),
		"xoxb-1234567890-abcdef",
		"AIzaSyA1234567890abcdefghijklmnopqrstuv",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature123",
		"https://hooks.slack.com/services/T000/B000/XXXX",
	}
	for _, in := range cases {
		if !LooksLike([]byte(in)) {
			t.Errorf("LooksLike(%q) = false, want true", in)
		}
	}
}

func TestLooksLikeIgnoresOrdinaryProse(t *testing.T) {
	cases := []string{
		"# Password rotation\n\nUse the secret manager.\n",
		"token: required for authentication",
		"password: too-short",
		"An API key is issued by the platform team.",
		"api_key = short",
		"-----BEGIN PUBLIC KEY-----\nnot a secret\n",
	}
	for _, in := range cases {
		if LooksLike([]byte(in)) {
			t.Errorf("LooksLike(%q) = true, want false", in)
		}
	}
}

func TestLooksLikeDetectsUTF16Credential(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 30)
	encoded := make([]byte, 0, len(token)*2)
	for i := 0; i < len(token); i++ {
		encoded = append(encoded, token[i], 0)
	}
	if !LooksLike(encoded) {
		t.Fatal("UTF-16 credential was not detected")
	}
}

func TestLooksLikeHandlesEmptyInput(t *testing.T) {
	if LooksLike(nil) || LooksLike([]byte{}) {
		t.Fatal("empty input should not look like a secret")
	}
}
