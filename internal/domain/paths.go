package domain

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Safe relative paths: forward-slash form, validated once after decoding.
// New names use Unicode NFC; case-fold collisions are refused inside a
// space so exports work on case-insensitive filesystems.

var reservedNames = map[string]bool{
	".netsidian": true,
	"thumbs.db":  true,
}

var invalidSegChars = regexp.MustCompile(`[\x00-\x1f\x7f\\:*?"<>|]`)

// NormalizePath validates and canonicalizes a client-supplied relative
// path. It returns the canonical path and its case-fold collision key.
func NormalizePath(p string) (canon string, key string, err *Error) {
	if p == "" || len(p) > 1024 {
		return "", "", Invalid("path must be non-empty and at most 1024 bytes")
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return "", "", Invalid("path must be relative and use forward slashes")
	}
	if strings.ContainsRune(p, '%') {
		// Decoding happens once at the transport boundary; encoded
		// sequences here would double-decode later, so refuse.
		return "", "", Invalid("percent-encoded paths are not accepted")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", "", Invalid("path must stay inside the space")
	}
	segs := strings.Split(cleaned, "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return "", "", Invalid("path must not contain empty or dot segments")
		}
		s = norm.NFC.String(s)
		if s == ".." || s == "." {
			return "", "", Invalid("path must not traverse")
		}
		if strings.HasPrefix(s, ".") && reservedNames[strings.ToLower(s)] {
			return "", "", Invalid("reserved path")
		}
		if strings.EqualFold(s, ".netsidian") {
			return "", "", Invalid("reserved namespace")
		}
		if invalidSegChars.MatchString(s) {
			return "", "", Invalid("path contains invalid characters")
		}
		for _, r := range s {
			if unicode.IsControl(r) {
				return "", "", Invalid("path contains control characters")
			}
		}
		s = strings.TrimRight(s, " ")
		if s == "" {
			return "", "", Invalid("path segment must not be blank")
		}
		out = append(out, s)
	}
	canon = strings.Join(out, "/")
	key = strings.ToLower(norm.NFD.String(canon)) + "\x00" + foldKey(canon)
	return canon, key, nil
}

// foldKey approximates case-folding collision detection for common
// case-insensitive filesystems.
func foldKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ValidNotePath requires a Markdown file extension.
func ValidNotePath(canon string) bool {
	return strings.HasSuffix(strings.ToLower(canon), ".md") && !strings.HasSuffix(strings.ToLower(canon), ".canvas.md")
}

// BaseName returns the final path segment without extension.
func BaseName(canon string) string {
	base := path.Base(canon)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".md"), ".MD")
}
