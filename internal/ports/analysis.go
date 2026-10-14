package ports

// Phase-2 W04 analysis contract (BEHAVIOR-CONTRACTS.md C02). The legacy
// Graph endpoint keeps its shape; this contract lives on the separate
// analysis route and response version so basic consumers never break.

// GraphQuery is the validated analysis query for one vault.
type GraphQuery struct {
	Query string `json:"query,omitempty"`

	Scope     string   `json:"scope"` // vault | note | selection | folder
	ScopeIDs  []string `json:"scopeIds,omitempty"`
	ScopePath string   `json:"scopePath,omitempty"`
	Mode      string   `json:"mode"`      // document (W04 baseline)
	Direction string   `json:"direction"` // in | out | both
	Depth     int      `json:"depth"`
	Limit     int      `json:"limit"`
}

// GraphEvidence locates the authorized source of a relationship.
type GraphEvidence struct {
	DocumentID string `json:"documentId"`
	Path       string `json:"path"`
	Revision   string `json:"revision"`
	Locator    string `json:"locator,omitempty"` // fragment or link target
	Excerpt    string `json:"excerpt,omitempty"` // bounded excerpt
	Method     string `json:"method"`            // explicit-link
	Stale      bool   `json:"stale,omitempty"`   // source revision no longer current
}

// AnalysisNode is a stable node: a document or an unresolved link target.
type AnalysisNode struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"` // document | unresolved
	Label      string   `json:"label"`
	DocumentID string   `json:"documentId,omitempty"`
	Path       string   `json:"path,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Importance int      `json:"importance"` // degree on the returned graph
	Metric     string   `json:"metric"`     // "Degree"
}

// AnalysisEdge is a consistent edge whose endpoints always exist in Nodes.
type AnalysisEdge struct {
	ID       string          `json:"id"`
	Source   string          `json:"source"`
	Target   string          `json:"target"`
	Type     string          `json:"type"` // explicit-link
	Weight   int             `json:"weight"`
	Evidence []GraphEvidence `json:"evidence,omitempty"`
}

type AnalysisCounts struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
}

type AnalysisLimits struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
}

// AnalysisGraph is the C02 response envelope.
type AnalysisGraph struct {
	Version           int            `json:"version"` // response version, currently 1
	Query             GraphQuery     `json:"query"`   // effective (normalized) query
	Status            string         `json:"status"`  // ready
	Nodes             []AnalysisNode `json:"nodes"`
	Edges             []AnalysisEdge `json:"edges"`
	Matched           AnalysisCounts `json:"matched"`           // authorized scope size pre-cap
	UnresolvedMatched AnalysisCounts `json:"unresolvedMatched"` // unresolved candidates before caps
	Returned          AnalysisCounts `json:"returned"`          // post-cap
	Truncated         bool           `json:"truncated"`
	Reason            string         `json:"truncationReason,omitempty"`
	Limits            AnalysisLimits `json:"limits"`
}

// LinkRow is one indexed link with its resolution state and revision.
type LinkRow struct {
	SourceID       string
	SourceRevision string
	State          string // resolved | unresolved | ambiguous
	TargetPath     string
	TargetBasename string
	Fragment       string
	ResolvedID     string
}

// DocLite is the minimal per-document record needed for graph scoping.
type DocLite struct {
	ID       string
	Path     string
	Title    string
	Revision string
	Tags     []string
	Memory   bool
}
