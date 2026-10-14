package markdown

import (
	"strings"
	"testing"
)

func TestParseExtractsStructure(t *testing.T) {
	src := "---\ntags: [alpha, beta]\n---\n# My Title\n\nSome **bold** and [[Other Note|alias]] text #inline.\n\n## Section One\n\n- [x] done\n- [ ] open\n\nLink [[dir/sub note#frag]] here.\n"
	idx := Parse(src, "Fallback")
	if idx.Title != "My Title" {
		t.Errorf("title=%q", idx.Title)
	}
	wantTags := map[string]bool{"alpha": false, "beta": false, "inline": false}
	for _, tag := range idx.Tags {
		wantTags[tag] = true
	}
	for tag, seen := range wantTags {
		if !seen {
			t.Errorf("missing tag %q in %v", tag, idx.Tags)
		}
	}
	if len(idx.Headings) < 2 {
		t.Errorf("headings=%v", idx.Headings)
	}
	if len(idx.Wikilinks) != 2 {
		t.Fatalf("wikilinks=%d %v", len(idx.Wikilinks), idx.Wikilinks)
	}
	if idx.Wikilinks[0].Target != "Other Note" {
		t.Errorf("first target=%q", idx.Wikilinks[0].Target)
	}
	if idx.Wikilinks[1].Target != "dir/sub note" || idx.Wikilinks[1].Fragment != "frag" {
		t.Errorf("second=%+v", idx.Wikilinks[1])
	}
	if idx.TasksDone != 1 || idx.TasksTotal != 2 {
		t.Errorf("tasks=%d/%d", idx.TasksDone, idx.TasksTotal)
	}
	if strings.Contains(idx.BodyPlain, "**") || strings.Contains(idx.BodyPlain, "[[") {
		t.Errorf("body not stripped: %q", idx.BodyPlain)
	}
}

func TestParseFallbackTitle(t *testing.T) {
	idx := Parse("body without heading\n", "My File")
	if idx.Title != "My File" {
		t.Errorf("fallback title=%q", idx.Title)
	}
}

func TestParseFrontmatterMalformed(t *testing.T) {
	fm, body, diag := ParseFrontmatter("---\nbroken: [unclosed\n")
	if len(diag) == 0 {
		t.Error("expected diagnostics for malformed frontmatter")
	}
	_ = fm
	_ = body
}

func TestPlainInlineNoBackreferencePanic(t *testing.T) {
	out := plainInline("a *i* b **bo** c __bu__ d __**mix**__ e")
	for _, junk := range []string{"*", "_"} {
		if strings.Contains(out, junk) {
			t.Errorf("emphasis markers remain: %q", out)
			break
		}
	}
}

func TestConsistentTagsAndCodeExclusion(t *testing.T) {
	idx := Parse("---\ntag: '#Work, project/one'\ntags: [WORK, Żółć, 123]\n---\n#work #-keep- #project/two #123\n`` #fake ` #fake ``\n    #indented\n<!-- #comment\n#hidden -->", "Test")
	got := strings.Join(idx.Tags, ",")
	if got != "-keep-,project/one,project/two,work,żółć" {
		t.Fatalf("unexpected tags: %s", got)
	}
}
func TestRootMarkdownLinksWithFragments(t *testing.T) {
	idx := Parse("[read](/Folder/Some%20Note.md#section)", "Test")
	if len(idx.Wikilinks) != 1 || idx.Wikilinks[0].Target != "/Folder/Some Note.md" || idx.Wikilinks[0].Fragment != "section" {
		t.Fatalf("unexpected links: %+v", idx.Wikilinks)
	}
}

func TestCanonicalTagValidationAndLongFences(t *testing.T) {
	idx := Parse("---\ntags: ['#work', 'invalid#', 'a//b', 'Ⅷ']\n---\n````\n```\n#hidden\n````\n#visible", "Test")
	if got := strings.Join(idx.Tags, ","); got != "visible,work" {
		t.Fatalf("unexpected tags: %s", got)
	}
}

func TestMemoDisplayTitle(t *testing.T) {
	idx := Parse("---\ntags: [memo]\n---\nA **quick thought** about navigation.\n\nAnother line.", "2026-10-10-random-id")
	if idx.Title != "A quick thought about navigation." {
		t.Fatalf("title=%q", idx.Title)
	}
	if got := Parse("---\ntags: [memo]\ntitle: Explicit name\n---\nBody", "fallback").Title; got != "Explicit name" {
		t.Fatalf("explicit title lost: %q", got)
	}
}
