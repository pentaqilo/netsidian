package markdown

import (
	"strings"
	"testing"
)

func TestMoveLinksPreservesDocumentsAttachmentsAndExamples(t *testing.T) {
	from, to := "Tasks/Board/todo/A.md", "Tasks/Board/done/A.md"
	paths := []string{from, "Tasks/Board/todo/B.md", "Tasks/Board/assets/Screen shot.png"}
	text := "---\nexample: '[[A]]'\n---\n# Café\n[[B#heading|Alias]] ![Shot](<../assets/Screen shot.png>) [Self](#section) [Site](https://example.test/A.md)\n`[[B]]`\n```md\n[[B]]\n```\n<!-- [[B]] -->\n[reference]: B.md \"title\"\n"
	got := RewriteMovedLinks(text, from, from, to, paths)
	for _, want := range []string{"[[/Tasks/Board/todo/B#heading|Alias]]", "![Shot](</Tasks/Board/assets/Screen%20shot.png>)", "[Self](/Tasks/Board/done/A.md#section)", "[Site](https://example.test/A.md)", "`[[B]]`", "```md\n[[B]]\n```", "<!-- [[B]] -->", "example: '[[A]]'", "[reference]: /Tasks/Board/todo/B.md \"title\"", "# Café"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	incoming := RewriteMovedLinks("[Task](Tasks/Board/todo/A.md#start) [[/Tasks/Board/todo/A|Task]]", "Index.md", from, to, paths)
	if !strings.Contains(incoming, "[Task](/Tasks/Board/done/A.md#start)") || !strings.Contains(incoming, "[[/Tasks/Board/done/A|Task]]") {
		t.Fatal(incoming)
	}
}
