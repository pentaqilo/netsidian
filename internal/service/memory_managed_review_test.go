package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMemoryManagedTargetPreservesHumanSectionsAndMetadata(t *testing.T) {
	original := "---\n# Human comment\ntitle: Human title\ncustom:\n  number: 9007199254740993\nnetsidian:\n  unrelated: keep\n---\nHuman intro **unchanged**.\n"
	if _, err := managedMemoryTarget(original, "Summary", "op-1", nil, nil, false); err == nil {
		t.Fatal("unowned target must require adoption")
	}
	claims := []MemoryClaimRef{{VaultID: "spc-1", IndexDocumentID: "doc-index", Key: "decision", IndexRevision: "rev-1", Fingerprint: "fp", ExpectedStatus: "active"}}
	first, err := managedMemoryTarget(original, "Summary", "op-1", []MemorySourceRef{{DocumentID: "doc-source", Revision: "rev-source"}}, claims, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "# Human comment") || !strings.Contains(first, "9007199254740993") || !strings.Contains(first, "unrelated: keep") || !strings.Contains(first, "Human intro **unchanged**.\n") {
		t.Fatal(first)
	}
	first += "\nHuman outro.\n"
	second, err := managedMemoryTarget(first, "New summary", "op-2", nil, claims, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(second, memoryManagedStart) != 1 || !strings.HasSuffix(second, "\nHuman outro.\n") || !strings.Contains(second, "Human intro **unchanged**.\n") || strings.Contains(second, "\nSummary\n") {
		t.Fatal(second)
	}
	fm := strings.SplitN(second, "---\n", 3)[1]
	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(fm), &decoded); err != nil {
		t.Fatal(err)
	}
	ns := decoded["netsidian"].(map[string]any)
	derived := ns["derived"].(map[string]any)
	claim := derived["claims"].([]any)[0].(map[string]any)
	if claim["indexDocumentId"] != "doc-index" || claim["indexRevision"] != "rev-1" || claim["expectedStatus"] != "active" {
		t.Fatal(claim)
	}
}
func TestMemoryManagedTargetRejectsDelimiterInjection(t *testing.T) {
	for _, body := range []string{memoryManagedStart, memoryManagedEnd} {
		if _, err := managedMemoryTarget("", body, "op", nil, nil, true); err == nil {
			t.Fatal("injected ownership delimiter accepted")
		}
	}
	for _, original := range []string{memoryManagedStart, memoryManagedEnd + memoryManagedStart, memoryManagedStart + memoryManagedEnd + memoryManagedStart + memoryManagedEnd} {
		if _, err := managedMemoryTarget(original, "body", "op", nil, nil, true); err == nil {
			t.Fatal("ambiguous delimiters accepted")
		}
	}
}
func TestMemoryClaimFingerprintSeparatesStatusFromSemantics(t *testing.T) {
	a, _ := parseMemoryLine(`- memory: {"key":"a","text":"Claim","status":"active","recordedAt":"2026-01-01T00:00:00Z","custom":{"number":9007199254740993}}`)
	b, _ := parseMemoryLine(`- memory: {"key":"a","text":"Claim","status":"superseded","recordedAt":"2026-01-02T00:00:00Z","custom":{"number":9007199254740993}}`)
	if claimFingerprint(a) != claimFingerprint(b) {
		t.Fatal("planned status transitions must use expectedStatus separately")
	}
	b.text = "Changed claim"
	if claimFingerprint(a) == claimFingerprint(b) {
		t.Fatal("changed semantic claim kept same fingerprint")
	}
}

func TestMemoryManagedTargetRefusesFutureMarker(t *testing.T) {
	original := "---\nnetsidian:\n  derived:\n    schemaVersion: 99\n---\nHuman content.\n"
	if _, err := managedMemoryTarget(original, "New body", "op", nil, nil, true); err == nil {
		t.Fatal("future derived metadata must not be overwritten even during adoption")
	}
}
