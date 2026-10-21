// Package markdown implements the Go side of the declared Netsidian
// Markdown profile used for indexing: frontmatter, headings, wikilinks,
// markdown links, tags and tasks. Graph extraction, backlinks and search
// share these fixtures so interpretations cannot diverge from rendering.
package markdown

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/pentaqilo/netsidian/internal/ports"
	"gopkg.in/yaml.v3"
)

var (
	// [[target]], [[target#fragment]], [[target|alias]], [[target#frag|alias]], ![[embed]]
	wikilinkRe = regexp.MustCompile(`(!?)\[\[([^\[\]|\n#]+)((#[^\[\]|\n]*)?)(\|([^\[\]|\n]*))?\]\]`)
	mdLinkRe   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	tagRe      = regexp.MustCompile(`(?:^|[\s(])#([\p{L}\p{N}_/\-]+)`)
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	taskRe     = regexp.MustCompile(`^\s*[-*+]\s+\[( |x|X)\]`)
	fenceRe    = regexp.MustCompile(`^\s*(` + "`{3,}" + `|~{3,})`)

	// Memory index entries reference documents by stable ID. Wrapped
	// references are ordinary wikilinks; legacy bare IDs on an entry line
	// ("`- memory: `") are extracted here so graph and backlinks see them
	// too. IDs elsewhere in prose stay literal text on purpose.
	memoryEntryRe = regexp.MustCompile(`^\s*-\s+memory:\s`)
	docIDRe       = regexp.MustCompile(`^doc_[a-z0-9]{26}$`)

	aliasLinkRe = regexp.MustCompile(`!?\[\[([^\]|#]+)(#[^\]|]*)?\|([^\]]+)\]\]`)
	plainLinkRe = regexp.MustCompile(`!?\[\[([^\]|#]+)(#[^\]|]*)?\]\]`)
	mdTextRe    = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	// RE2 has no backreferences, so star/underscore emphasis are split.
	boldStarRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	boldUndRe  = regexp.MustCompile(`__(.+?)__`)
	italStarRe = regexp.MustCompile(`(^|[\s(])\*([^*_\s]+)\*`)
	italUndRe  = regexp.MustCompile(`(^|[\s(])_([^*_\s]+)_`)
	hlRe       = regexp.MustCompile(`==(.*?)==`)
)

// ParseFrontmatter splits YAML frontmatter from the body without
// destroying bytes the renderer needs. Malformed YAML yields a
// diagnostic while keeping the original text visible.
func ParseFrontmatter(src string) (fm map[string]any, body string, diagnostics []string) {
	fm = map[string]any{}
	src = strings.TrimPrefix(src, "\uFEFF")
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return fm, src, nil
	}
	rest := src[4:]
	lines := strings.Split(rest, "\n")
	end := -1
	for i, l := range lines {
		t := strings.TrimRight(l, "\r")
		if t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return fm, src, []string{"unterminated frontmatter"}
	}
	fmText := strings.Join(lines[:end], "\n")
	body = strings.Join(lines[end+1:], "\n")
	if strings.TrimSpace(fmText) == "" {
		return fm, body, nil
	}
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		// never hide the body or destroy original bytes on bad YAML
		return map[string]any{}, src, []string{"malformed YAML frontmatter: " + err.Error()}
	}
	return fm, body, nil
}

// Parse derives the full derived-metadata payload for one committed
// revision. Code fence content never creates structure.
func Parse(src, fallbackTitle string) *ports.IndexData {
	fm, body, diags := ParseFrontmatter(src)
	idx := &ports.IndexData{Frontmatter: fm, Diagnostics: diags}
	seenTags := map[string]bool{}
	inFence := false
	fenceChar := ""
	lines := strings.Split(body, "\n")
	var plain strings.Builder
	inComment := false
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if f := fenceRe.FindString(line); f != "" {
			t := strings.TrimSpace(line)
			if !inFence {
				inFence = true
				fenceChar = strings.TrimSpace(f)
			} else if strings.HasPrefix(t, fenceChar) && strings.TrimSpace(strings.TrimPrefix(t, fenceChar)) == "" {
				inFence = false
			}
			continue
		}
		if inFence || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if h := headingRe.FindStringSubmatch(line); h != nil {
			text := plainInline(h[2])
			idx.Headings = append(idx.Headings, ports.Heading{
				Level: len(h[1]), Text: text, Anchor: Anchor(text), Line: i + 1,
			})
		}
		if tk := taskRe.FindStringSubmatch(line); tk != nil {
			idx.TasksTotal++
			if tk[1] == "x" || tk[1] == "X" {
				idx.TasksDone++
			}
		}
		clean := blankInlineCode(line)
		clean = blankComments(clean, &inComment)
		for _, m := range wikilinkRe.FindAllStringSubmatch(clean, -1) {
			target := strings.TrimSpace(m[2])
			if target == "" {
				continue
			}
			ref := ports.LinkRef{Target: target}
			if m[3] != "" {
				ref.Fragment = strings.TrimPrefix(m[3], "#")
			}
			if m[6] != "" {
				ref.Alias = strings.TrimSpace(m[6])
			}
			idx.Wikilinks = append(idx.Wikilinks, ref)
		}
		if mm := memoryEntryRe.FindStringSubmatchIndex(clean); mm != nil {
			// Legacy bare stable-ID refs/details: only the structured
			// ref/detail fields are references; IDs quoted inside entry
			// text stay literal. Bracket-wrapped refs are already captured
			// as wikilinks above, so only unwrapped IDs are added here.
			var entry struct {
				Key    string   `json:"key"`
				Text   string   `json:"text"`
				Refs   []string `json:"refs"`
				Detail string   `json:"detail"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(line[mm[1]:])), &entry) == nil && entry.Key != "" && entry.Text != "" {
				for _, target := range append(entry.Refs, entry.Detail) {
					if strings.Contains(target, "[[") {
						continue
					}
					if docIDRe.MatchString(target) {
						idx.Wikilinks = append(idx.Wikilinks, ports.LinkRef{Target: target})
					}
				}
			}
		}
		for _, m := range mdLinkRe.FindAllStringSubmatch(clean, -1) {
			url := m[1]
			switch {
			case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"), strings.HasPrefix(url, "mailto:"):
				idx.Wikilinks = append(idx.Wikilinks, ports.LinkRef{Target: url, IsURL: true})
			default:
				target, fragment, _ := strings.Cut(url, "#")
				if strings.HasSuffix(strings.ToLower(target), ".md") {
					if decoded, err := urlpkgPathUnescape(target); err == nil {
						target = decoded
					}
					idx.Wikilinks = append(idx.Wikilinks, ports.LinkRef{Target: target, Fragment: fragment})
				}
			}
		}
		for _, m := range tagRe.FindAllStringSubmatch(clean, -1) {
			tag := normalizeTag(m[1])
			if tag == "" {
				continue
			}
			seenTags[tag] = true
		}
		plain.WriteString(plainInline(clean))
		plain.WriteByte('\n')
	}
	for _, t := range frontmatterTags(fm) {
		seenTags[t] = true
	}
	for t := range seenTags {
		idx.Tags = append(idx.Tags, t)
	}
	sort.Strings(idx.Tags)
	idx.BodyPlain = strings.TrimSpace(plain.String())
	idx.Title = firstHeadingTitle(idx.Headings)
	if idx.Title == "" {
		if st, ok := fm["title"].(string); ok && strings.TrimSpace(st) != "" {
			idx.Title = strings.TrimSpace(st)
		}
	}
	if idx.Title == "" && slices.Contains(idx.Tags, "memo") && idx.BodyPlain != "" {
		first := strings.TrimSpace(strings.SplitN(idx.BodyPlain, "\n", 2)[0])
		chars := []rune(first)
		if len(chars) > 80 {
			first = string(chars[:77]) + "…"
		}
		idx.Title = first
	}
	if idx.Title == "" {
		idx.Title = fallbackTitle
	}
	return idx
}

func first3(s string) string {
	if len(s) < 3 {
		return s
	}
	return s[:3]
}

func firstHeadingTitle(hs []ports.Heading) string {
	for _, h := range hs {
		if h.Level == 1 {
			return h.Text
		}
	}
	return ""
}

// blankInlineCode blanks code-span content so its text cannot create
// tags or links, while keeping character positions for readability.
func blankInlineCode(s string) string {
	var out strings.Builder
	delimiter := ""
	for i := 0; i < len(s); {
		if s[i] == '`' {
			end := i + 1
			for end < len(s) && s[end] == '`' {
				end++
			}
			run := s[i:end]
			if delimiter == "" {
				delimiter = run
			} else if delimiter == run {
				delimiter = ""
			}
			out.WriteString(strings.Repeat(" ", end-i))
			i = end
			continue
		}
		if delimiter != "" {
			out.WriteByte(' ')
		} else {
			out.WriteByte(s[i])
		}
		i++
	}
	return out.String()
}
func blankComments(s string, in *bool) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if *in {
			end := strings.Index(s[i:], "-->")
			if end < 0 {
				out.WriteString(strings.Repeat(" ", len(s)-i))
				break
			}
			out.WriteString(strings.Repeat(" ", end+3))
			i += end + 3
			*in = false
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			*in = true
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
func urlpkgPathUnescape(target string) (string, error) { return url.PathUnescape(target) }
func normalizeTag(s string) string {
	s = strings.ToLower(strings.Trim(strings.TrimLeft(strings.TrimSpace(s), "#"), "/"))
	if s == "" || isAllDigits(s) {
		return ""
	}
	for _, segment := range strings.Split(s, "/") {
		if segment == "" {
			return ""
		}
		for _, r := range segment {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-' {
				return ""
			}
		}
	}
	return s
}

// plainInline reduces inline markup to human-readable plain text.
func plainInline(s string) string {
	s = aliasLinkRe.ReplaceAllString(s, "$3")
	s = plainLinkRe.ReplaceAllString(s, "$1")
	s = mdTextRe.ReplaceAllString(s, "$1")
	s = boldStarRe.ReplaceAllString(s, "$1")
	s = boldUndRe.ReplaceAllString(s, "$1")
	s = italStarRe.ReplaceAllString(s, "$1$2")
	s = italUndRe.ReplaceAllString(s, "$1$2")
	s = hlRe.ReplaceAllString(s, "$1")
	return s
}

// frontmatterTags extracts tags/tag properties.
func frontmatterTags(fm map[string]any) []string {
	var out []string
	var add func(v any)
	add = func(v any) {
		switch t := v.(type) {
		case string:
			for _, part := range strings.FieldsFunc(t, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
				part = normalizeTag(part)
				if part != "" {
					out = append(out, part)
				}
			}
		case []any:
			for _, e := range t {
				add(e)
			}
		}
	}
	for _, key := range []string{"tags", "tag"} {
		if v, ok := fm[key]; ok {
			add(v)
		}
	}
	return out
}

// Anchor converts heading text to the shared anchor id used by both
// renderers and link fragments.
func Anchor(text string) string {
	var b strings.Builder
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, r := range lower {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	a := strings.Trim(b.String(), "-")
	if a == "" {
		a = "section"
	}
	return a
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}
