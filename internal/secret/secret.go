// Package secret contains companion's local credential heuristic. It is a
// safety net for writes and commits, not a complete secret manager: it looks
// for common credential formats and key/value pairs that carry a long value.
package secret

import (
	"bytes"
	"regexp"
	"strings"
)

var patterns = []*regexp.Regexp{
	// AWS, API keys, passwords and similar key/value pairs. The value must be
	// long enough to avoid matching prose such as "token: required".
	regexp.MustCompile(`(?i)\b(aws_secret_access_key|aws_access_key_id|secret_access_key|secret_key|access[_-]?token|auth[_-]?token|refresh[_-]?token|api[_-]?key|apikey|client[_-]?secret|private[_-]?key|password|passwd)\b\s*[:=]\s*["']?[A-Za-z0-9/+_.~\-]{16,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}\b`),
	regexp.MustCompile(`\bsk_live_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`),
	regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY(?: BLOCK)?-----`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`),
	regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`),
}

// LooksLike reports whether data appears to contain a credential. A NUL byte
// is treated as UTF-16 padding: the ASCII prefixes remain visible after the
// padding is removed, which prevents a UTF-16 secret from bypassing the scan.
func LooksLike(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	s := string(data)
	if bytes.IndexByte(data, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
