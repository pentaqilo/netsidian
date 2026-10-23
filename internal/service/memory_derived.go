package service

import (
	"context"
	"sort"
)

// Derived memory documents declare their inputs in frontmatter:
//
//	netsidian:
//	  derived:
//	    sources:
//	      - documentId: doc_x
//	        revision: rev_y
//
// The marker is deliberately small: storage keeps no dependency table.
// Status is computed on demand from bounded indexed metadata, which
// keeps the marker honest (it is plain content the user can edit) and
// avoids a second source of truth that could drift from revisions.

// DerivedSourceStatus is one pinned input of a derived document.
type DerivedSourceStatus struct {
	DocumentID      string `json:"documentId"`
	Path            string `json:"path,omitempty"`
	PinnedRevision  string `json:"pinnedRevision"`
	CurrentRevision string `json:"currentRevision,omitempty"`
	Available       bool   `json:"available"`
	Stale           bool   `json:"stale"`
}

// DerivedDocumentStatus is the computed health of one derived document.
// Status is current, stale (a pinned input moved), broken (an input is
// gone) or cyclic (dependency cycle).
type DerivedDocumentStatus struct {
	DocumentID string                `json:"documentId"`
	Path       string                `json:"path"`
	Revision   string                `json:"revision"`
	Status     string                `json:"status"`
	Sources    []DerivedSourceStatus `json:"sources"`
	Cycle      []string              `json:"cycle,omitempty"`
}

type MemoryDerivedResult struct {
	Scanned   int                     `json:"scanned"`
	Derived   []DerivedDocumentStatus `json:"derived"`
	Truncated bool                    `json:"truncated,omitempty"`
}

// DerivedStatus reports derived-document health using only the bounded
// indexed candidate pages; it never scans document bodies. Claim
// dependencies recorded in the marker participate in staleness.
func (s *Services) DerivedStatus(ctx context.Context, a *Actor, spaceID string) (*MemoryDerivedResult, error) {
	page, err := s.DerivedStatusWithInput(ctx, a, spaceID, MemoryDerivedInput{Limit: 50, MaxBytes: 32768})
	if err != nil {
		return nil, err
	}
	result := &MemoryDerivedResult{Scanned: page.Scanned, Derived: page.Derived, Truncated: page.Truncated}

	sort.Slice(result.Derived, func(i, j int) bool { return result.Derived[i].Path < result.Derived[j].Path })
	return result, nil
}

func indexOfStr(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
