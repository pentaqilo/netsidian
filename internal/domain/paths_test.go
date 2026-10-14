package domain

import "testing"

func TestNormalizePathAccepts(t *testing.T) {
	for _, p := range []string{"a.md", "Notes/Hello.md", "a b/c-d.md", "Тест/笔记.md", ".hidden/x.md"} {
		if _, _, err := NormalizePath(p); err != nil {
			t.Errorf("should accept %q: %s", p, err.Message)
		}
	}
}

func TestNormalizePathRejects(t *testing.T) {
	for _, p := range []string{
		"", "/abs.md", "..\\x.md", "../escape.md", "..", ".",
		"x%.md", ".netsidian/x.md", "a\tx.md", "trail/ ",
	} {
		if canon, key, err := NormalizePath(p); err == nil {
			t.Errorf("should reject %q (canon=%q key=%q)", p, canon, key)
		}
	}
}

func TestNormalizePathCollapsesDotSegments(t *testing.T) {
	for _, tc := range [][2]string{
		{"a/../b.md", "b.md"},
		{"a//b.md", "a/b.md"},
		{"a/b/./c.md", "a/b/c.md"},
		{"Folder/./Note.md", "Folder/Note.md"},
	} {
		canon, key, err := NormalizePath(tc[0])
		if err != nil {
			t.Errorf("%q: %s", tc[0], err.Message)
			continue
		}
		if canon != tc[1] {
			t.Errorf("canon(%q)=%q want %q", tc[0], canon, tc[1])
		}
		if key == "" {
			t.Errorf("empty key for %q", tc[0])
		}
	}
}

func TestNormalizePathTrimsTrailingSpace(t *testing.T) {
	canon, _, err := NormalizePath("x.md ")
	if err != nil {
		t.Fatal(err.Message)
	}
	if canon != "x.md" {
		t.Errorf("canonical=%q want x.md", canon)
	}
}

func TestNormalizePathKeyDeterministic(t *testing.T) {
	_, k1, e := NormalizePath("Folder/Note.md")
	if e != nil {
		t.Fatal(e.Message)
	}
	_, k2, _ := NormalizePath("/../x.md") // invalid path must yield empty key
	if k2 != "" {
		t.Error("invalid path must yield empty key")
	}
	if k1 == "" {
		t.Fatal("key empty")
	}
	_, k3, _ := NormalizePath("Folder/Note.md")
	if k1 != k3 {
		t.Error("key must be deterministic")
	}
}
