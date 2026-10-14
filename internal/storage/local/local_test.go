package local

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func newContent(t *testing.T) *Content {
	t.Helper()
	c, err := NewContent(filepath.Join(t.TempDir(), "content"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestWriteAtomicRoundTrip(t *testing.T) {
	c := newContent(t)
	if err := c.WriteAtomic("sp1", "Notes/a b.md", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Read("sp1", "Notes/a b.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	// no temp residue left behind
	entries, _ := os.ReadDir(filepath.Join(rootOf(t, c), "spaces", "sp1", "Notes"))
	for _, e := range entries {
		if e.Name() != "a b.md" {
			t.Errorf("unexpected residue %q", e.Name())
		}
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	c := newContent(t)
	for _, p := range []string{"../evil.md", "a/../../evil.md", "/etc/passwd", "..", "a/./../b/../../../x.md"} {
		if _, err := c.Read("sp1", p); err == nil {
			t.Errorf("traversal accepted: %q", p)
		}
	}
	if err := c.WriteAtomic("sp1", "../evil.md", []byte("x")); err == nil {
		t.Error("traversal write accepted")
	}
	// absolute path attempts
	if err := c.WriteAtomic("sp1", "/tmp/evil.md", []byte("x")); err == nil {
		t.Error("absolute write accepted")
	}
}

func TestSpaceIsolation(t *testing.T) {
	c := newContent(t)
	if err := c.WriteAtomic("sp1", "x.md", []byte("one")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Read("sp2", "x.md")
	if err == nil {
		t.Errorf("cross-space read succeeded: %q", got)
	}
	// space ids are validated; odd ids must not escape the root either
	for _, sid := range []string{"..", "../escape", "/abs"} {
		if err := c.WriteAtomic(sid, "x.md", []byte("x")); err == nil {
			t.Errorf("bad space id accepted: %q", sid)
		}
	}
}

func TestMoveAndDelete(t *testing.T) {
	c := newContent(t)
	if err := c.WriteAtomic("sp1", "a.md", []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := c.Move("sp1", "a.md", ".netsidian-trash/1/a.md"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Exists("sp1", "a.md"); ok {
		t.Error("source still exists after move")
	}
	got, err := c.Read("sp1", ".netsidian-trash/1/a.md")
	if err != nil || !bytes.Equal(got, []byte("data")) {
		t.Errorf("moved content wrong: %q %v", got, err)
	}
	if err := c.Delete("sp1", ".netsidian-trash/1/a.md"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Exists("sp1", ".netsidian-trash/1/a.md"); ok {
		t.Error("still exists after delete")
	}
}

func rootOf(t *testing.T, c *Content) string {
	t.Helper()
	// resolve returns absolute filesystem paths under <root>/spaces/<space>/
	p, err := c.resolve("sp1", "x.md")
	if err != nil {
		t.Fatal(err)
	}
	spaces := filepath.Dir(filepath.Dir(p)) // strip <space>/x.md
	return filepath.Dir(spaces)             // strip spaces/ -> root
}
