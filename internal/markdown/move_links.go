package markdown

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

var moveMarkdownLink = regexp.MustCompile(`!?\[[^\]\n]*\]\((<[^>\n]+>|[^\s)\n]+)`)
var moveReferenceLink = regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]\n]+\]:[ \t]+(<[^>\n]+>|[^\s\n]+)`)

// RewriteMovedLinks preserves resolved local links when a document moves.
// paths includes documents and attachments before the move. Code examples,
// comments, frontmatter, external URLs and unresolved targets remain intact.
func RewriteMovedLinks(text, source, from, to string, paths []string) string {
	masked := moveProseMask(text)
	type edit struct {
		start, end int
		value      string
	}
	var edits []edit
	resolve := func(raw string) (string, string) {
		raw = strings.TrimSpace(raw)
		if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
			raw = raw[1 : len(raw)-1]
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(raw, "//") {
			return "", ""
		}
		name, err := url.PathUnescape(u.EscapedPath())
		if err != nil {
			return "", ""
		}
		suffix := ""
		if u.RawQuery != "" {
			suffix += "?" + u.RawQuery
		}
		if u.Fragment != "" {
			suffix += "#" + u.EscapedFragment()
		}
		if name == "" {
			if u.Fragment != "" {
				return source, suffix
			}
			return "", ""
		}
		candidates := []string{name}
		if path.Ext(name) == "" {
			candidates = append([]string{name + ".md"}, candidates...)
		}
		for _, n := range candidates {
			local := path.Clean(path.Join(path.Dir(source), n))
			if strings.HasPrefix(n, "/") {
				local = strings.TrimPrefix(path.Clean(n), "/")
			}
			root := strings.TrimPrefix(path.Clean(n), "/")
			for _, p := range paths {
				if p == local || p == root {
					return p, suffix
				}
			}
		}
		if !strings.HasPrefix(name, "/") {
			var match string
			for _, p := range paths {
				if strings.TrimSuffix(path.Base(p), ".md") == strings.TrimSuffix(path.Base(name), ".md") {
					if match != "" {
						return "", ""
					}
					match = p
				}
			}
			return match, suffix
		}
		return "", ""
	}
	for _, m := range wikilinkRe.FindAllStringSubmatchIndex(masked, -1) {
		if moveLinkEscaped(text, m[0]) {
			continue
		}
		raw := text[m[4]:m[5]]
		resolved, _ := resolve(raw)
		if resolved == "" || (source != from && resolved != from) {
			continue
		}
		dest := resolved
		if dest == from {
			dest = to
		}
		// Replace only the target, retaining embed marker, fragment and alias.
		edits = append(edits, edit{m[4], m[5], "/" + strings.TrimSuffix(dest, ".md")})
	}
	for _, re := range []*regexp.Regexp{moveMarkdownLink, moveReferenceLink} {
		for _, m := range re.FindAllStringSubmatchIndex(masked, -1) {
			if moveLinkEscaped(text, m[0]) || (re == moveReferenceLink && strings.HasPrefix(strings.TrimLeft(text[m[0]:m[1]], " \t"), "[^")) {
				continue
			}
			raw := text[m[2]:m[3]]
			resolved, suffix := resolve(raw)
			if resolved == "" || (source != from && resolved != from) {
				continue
			}
			dest := resolved
			if dest == from {
				dest = to
			}
			value := (&url.URL{Path: "/" + dest}).EscapedPath() + suffix
			if strings.HasPrefix(raw, "<") {
				value = "<" + value + ">"
			}
			edits = append(edits, edit{m[2], m[3], value})
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		text = text[:e.start] + e.value + text[e.end:]
	}
	return text
}

func moveProseMask(text string) string {
	// Byte offsets must stay identical even for non-ASCII content.
	bytesBlank := func(s string) string {
		b := []byte(s)
		for i := range b {
			if b[i] != '\n' && b[i] != '\r' {
				b[i] = ' '
			}
		}
		return string(b)
	}
	out := []byte(text)
	offset := 0
	fence := ""
	comment := false
	frontmatter := false
	for i, line := range strings.SplitAfter(text, "\n") {
		clean := strings.TrimSpace(line)
		masked := line
		if i == 0 && strings.TrimPrefix(clean, "\uFEFF") == "---" {
			frontmatter = true
			masked = bytesBlank(line)
		} else if frontmatter {
			masked = bytesBlank(line)
			if clean == "---" || clean == "..." {
				frontmatter = false
			}
		} else if m := fenceRe.FindString(line); m != "" {
			if fence == "" {
				fence = strings.TrimSpace(m)
			} else if strings.HasPrefix(clean, fence) && strings.TrimSpace(strings.TrimPrefix(clean, fence)) == "" {
				fence = ""
			}
			masked = bytesBlank(line)
		} else if fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			masked = bytesBlank(line)
		} else {
			masked = blankComments(blankInlineCode(line), &comment)
		}
		copy(out[offset:], masked)
		offset += len(line)
	}
	return string(out)
}

func moveLinkEscaped(text string, at int) bool {
	n := 0
	for i := at - 1; i >= 0 && text[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}
