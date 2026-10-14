package service

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"gopkg.in/yaml.v3"
)

// Folder-backed Kanban boards. A board is a folder that contains a board.md
// whose YAML frontmatter carries the versioned board configuration. Task
// status and membership are derived exclusively from where a document lives
// (which column folder it is under); the board.md only owns column labels,
// column order, completion categories, card ordering (by stable document id),
// soft WIP limits and creation defaults. It never duplicates authoritative
// membership or status.

// BoardColumn is one configured column of a board.
type BoardColumn struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Folder     string `json:"folder"`
	Completion bool   `json:"completion,omitempty"`
	WIPLimit   int    `json:"wipLimit,omitempty"`
}

// BoardCreateDefaults captures task-creation defaults for a board.
type BoardCreateDefaults struct {
	Column  string `json:"column,omitempty"`
	Content string `json:"content,omitempty"`
}

// BoardConfig is the parsed, validated board configuration.
type BoardConfig struct {
	Version        int                  `json:"version"`
	Title          string               `json:"title,omitempty"`
	Columns        []BoardColumn        `json:"columns"`
	Order          map[string][]string  `json:"order,omitempty"`
	CreateDefaults *BoardCreateDefaults `json:"createDefaults,omitempty"`
}

// BoardCard is one task card located on a board.
type BoardCard struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	Revision  string    `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// BoardColumnView is a column together with its resolved cards.
type BoardColumnView struct {
	BoardColumn
	Cards []BoardCard `json:"cards"`
}

// Board is the resolved board view returned to clients.
type Board struct {
	Path     string            `json:"path"`
	Title    string            `json:"title"`
	Revision string            `json:"revision"`
	Config   BoardConfig       `json:"config"`
	Columns  []BoardColumnView `json:"columns"`
	Overflow []BoardCard       `json:"overflow,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`
}

// BoardSummary is a lightweight board listing entry.
type BoardSummary struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Columns int    `json:"columns"`
}

const (
	boardFileName   = "board.md"
	boardAssetsName = "assets"
	boardMarkerKey  = "netsidian"
	boardMarkerVal  = "board"
)

func defaultBoardColumns() []BoardColumn {
	return []BoardColumn{
		{ID: "todo", Label: "To do", Folder: "todo"},
		{ID: "in-progress", Label: "In progress", Folder: "in-progress"},
		{ID: "done", Label: "Done", Folder: "done", Completion: true},
	}
}

// boardFolderRel validates a single column folder segment.
func validColumnFolder(folder string) error {
	canon, _, err := domain.NormalizePath(folder)
	if err != nil {
		return err
	}
	if canon != folder || len(folder) > 128 {
		return domain.Invalid("invalid column folder name")
	}
	if folder == "" {
		return domain.Invalid("column folder must not be empty")
	}
	if strings.Contains(folder, "/") || strings.Contains(folder, "\\") {
		return domain.Invalid("column folder must be a single folder name")
	}
	if folder == "." || folder == ".." {
		return domain.Invalid("column folder must not traverse")
	}
	if strings.HasPrefix(folder, ".") {
		return domain.Invalid("reserved column folder name")
	}
	if strings.EqualFold(folder, boardAssetsName) {
		return domain.Invalid("assets is reserved for non-task files")
	}
	if strings.EqualFold(folder, "board") {
		return domain.Invalid("board is reserved for the board configuration file")
	}
	return nil
}

func boardMDPath(boardPath string) string {
	return boardPath + "/" + boardFileName
}

// parseBoardConfig reads the board configuration out of parsed frontmatter.
// It returns ok=false when the frontmatter is not a board configuration.
func parseBoardConfig(fm map[string]any) (BoardConfig, bool) {
	marker, _ := fm[boardMarkerKey].(string)
	if !strings.EqualFold(strings.TrimSpace(marker), boardMarkerVal) {
		return BoardConfig{}, false
	}
	cfg := BoardConfig{Version: 1}
	if v, ok := asInt(fm["version"]); ok && v > 0 {
		cfg.Version = v
	}
	if t, ok := fm["title"].(string); ok {
		cfg.Title = strings.TrimSpace(t)
	}
	if raw, ok := fm["columns"]; ok {
		cfg.Columns = parseBoardColumns(raw)
	}
	if len(cfg.Columns) == 0 {
		cfg.Columns = defaultBoardColumns()
	}
	if raw, ok := fm["order"]; ok {
		cfg.Order = parseBoardOrder(raw)
	}
	if raw, ok := fm["createDefaults"]; ok {
		if m, ok := raw.(map[string]any); ok {
			col, _ := m["column"].(string)
			content, _ := m["content"].(string)
			cfg.CreateDefaults = &BoardCreateDefaults{Column: strings.TrimSpace(col), Content: content}
		}
	}
	return cfg, true
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n < 0 || n > 1000000 || n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}

func parseBoardColumns(raw any) []BoardColumn {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]BoardColumn, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		col := BoardColumn{ID: id}
		if label, ok := m["label"].(string); ok && strings.TrimSpace(label) != "" {
			col.Label = strings.TrimSpace(label)
		} else {
			col.Label = id
		}
		if folder, ok := m["folder"].(string); ok && strings.TrimSpace(folder) != "" {
			col.Folder = strings.TrimSpace(folder)
		} else {
			col.Folder = id
		}
		if c, ok := m["completion"].(bool); ok {
			col.Completion = c
		}
		if w, ok := asInt(m["wipLimit"]); ok && w > 0 {
			col.WIPLimit = w
		}
		out = append(out, col)
	}
	return out
}

func parseBoardOrder(raw any) map[string][]string {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string][]string{}
	for k, v := range m {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		ids := make([]string, 0, len(list))
		for _, e := range list {
			if s, ok := e.(string); ok && s != "" {
				ids = append(ids, s)
			}
		}
		if len(ids) > 0 {
			out[k] = ids
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validateBoardConfig enforces the structural invariants of a board config.
func validateBoardConfig(cfg BoardConfig) error {
	if cfg.Version != 1 {
		return domain.Unsupported("unsupported board configuration version")
	}
	if len(cfg.Columns) > 32 {
		return domain.Invalid("a board supports at most 32 columns")
	}
	if len(cfg.Columns) == 0 {
		return domain.Invalid("a board needs at least one column")
	}
	seenID := map[string]bool{}
	seenFolder := map[string]bool{}
	for _, c := range cfg.Columns {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).MatchString(c.ID) || c.Label == "" || len(c.Label) > 128 || c.WIPLimit < 0 || c.WIPLimit > 10000 {
			return domain.Invalid("column id must not be empty")
		}
		if seenID[c.ID] {
			return domain.Invalid("duplicate column id: " + c.ID)
		}
		seenID[c.ID] = true
		if err := validColumnFolder(c.Folder); err != nil {
			return err
		}
		fkey := normKeyOr(c.Folder)
		if seenFolder[fkey] {
			return domain.Invalid("duplicate column folder: " + c.Folder)
		}
		seenFolder[fkey] = true
	}
	if cfg.CreateDefaults != nil && cfg.CreateDefaults.Column != "" {
		if !seenID[cfg.CreateDefaults.Column] {
			return domain.Invalid("createDefaults.column refers to an unknown column")
		}
	}
	for colID := range cfg.Order {
		if !seenID[colID] {
			return domain.Invalid("order refers to an unknown column: " + colID)
		}
	}
	return nil
}

// boardFileYAML is the on-disk frontmatter shape for board.md.
type boardColumnYAML struct {
	ID         string `yaml:"id"`
	Label      string `yaml:"label,omitempty"`
	Folder     string `yaml:"folder,omitempty"`
	Completion bool   `yaml:"completion,omitempty"`
	WIPLimit   int    `yaml:"wipLimit,omitempty"`
}
type boardDefaultsYAML struct {
	Column  string `yaml:"column,omitempty"`
	Content string `yaml:"content,omitempty"`
}
type boardFileYAML struct {
	Netsidian    string              `yaml:"netsidian"`
	Version        int                 `yaml:"version"`
	Title          string              `yaml:"title,omitempty"`
	Columns        []boardColumnYAML   `yaml:"columns"`
	Order          map[string][]string `yaml:"order,omitempty"`
	CreateDefaults *boardDefaultsYAML  `yaml:"createDefaults,omitempty"`
}

// renderBoardMarkdown serializes a board config plus a preserved body.
func renderBoardMarkdown(cfg BoardConfig, body string) (string, error) {
	file := boardFileYAML{
		Netsidian: boardMarkerVal,
		Version:     cfg.Version,
		Title:       cfg.Title,
	}
	for _, c := range cfg.Columns {
		file.Columns = append(file.Columns, boardColumnYAML{
			ID: c.ID, Label: c.Label, Folder: c.Folder,
			Completion: c.Completion, WIPLimit: c.WIPLimit,
		})
	}
	if len(cfg.Order) > 0 {
		file.Order = cfg.Order
	}
	if cfg.CreateDefaults != nil {
		file.CreateDefaults = &boardDefaultsYAML{Column: cfg.CreateDefaults.Column, Content: cfg.CreateDefaults.Content}
	}
	head, err := yaml.Marshal(file)
	if err != nil {
		return "", domain.Invalid("could not serialize board configuration: " + err.Error())
	}
	out := "---\n" + string(head) + "---\n"
	if body != "" {
		out += body
	}
	return out, nil
}

// boardColumnForPath maps a document path to the configured column whose
// folder is the longest matching prefix under the board. Returns "" when the
// document is not inside any configured column folder.
func boardColumnForPath(cfg BoardConfig, boardPath, docPath string) string {
	rel := strings.TrimPrefix(docPath, boardPath+"/")
	if rel == docPath {
		return "" // not under the board
	}
	best := ""
	bestLen := -1
	for _, c := range cfg.Columns {
		prefix := c.Folder + "/"
		if strings.HasPrefix(rel, prefix) && len(c.Folder) > bestLen {
			best = c.ID
			bestLen = len(c.Folder)
		}
	}
	return best
}

// isBoardCardKind restricts cards to note/memory documents; attachments and
// other supporting files never become cards.
func isBoardCardKind(kind string) bool {
	return kind == "note" || kind == "memory"
}

// readBoardDocument resolves the board.md document for a board folder.
func (s *Services) readBoardDocument(ctx context.Context, a *Actor, spaceID, boardPath string) (*domain.Document, *NoteContent, error) {
	_, key, perr := domain.NormalizePath(boardMDPath(boardPath))
	if perr != nil {
		return nil, nil, perr
	}
	doc, err := s.Docs.DocumentByNormKey(ctx, spaceID, key)
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || doc.TombstoneAt != nil {
		return nil, nil, domain.NotFound("board not found")
	}
	nc, err := s.ReadNote(ctx, a, spaceID, doc.ID)
	if err != nil {
		return nil, nil, err
	}

	return doc, nc, nil
}

// loadBoardConfig refuses malformed or unsupported data before mutation.
func (s *Services) loadBoardConfig(ctx context.Context, a *Actor, spaceID, boardPath string) (*domain.Document, *NoteContent, BoardConfig, error) {
	doc, nc, err := s.readBoardDocument(ctx, a, spaceID, boardPath)
	if err != nil {
		return nil, nil, BoardConfig{}, err
	}
	fm, _, diagnostics := markdown.ParseFrontmatter(nc.Markdown)
	if len(diagnostics) > 0 {
		return nil, nil, BoardConfig{}, domain.Invalid("repair malformed board frontmatter in board.md")
	}
	cfg, ok := parseBoardConfig(fm)
	if !ok {
		return nil, nil, BoardConfig{}, domain.NotFound("board not found")
	}
	if raw, exists := fm["version"]; exists {
		v, valid := asInt(raw)
		if !valid || v != 1 {
			return nil, nil, BoardConfig{}, domain.Unsupported("unsupported board configuration version")
		}
	}
	if raw, exists := fm["columns"]; exists {
		cols, valid := raw.([]any)
		if !valid || len(cols) == 0 || len(cols) != len(cfg.Columns) {
			return nil, nil, BoardConfig{}, domain.Invalid("repair malformed board columns in board.md")
		}
	}
	if err := validateBoardConfig(cfg); err != nil {
		return nil, nil, BoardConfig{}, err
	}

	if raw, exists := fm["columns"]; exists {
		for _, entry := range raw.([]any) {
			m := entry.(map[string]any)
			if value, ok := m["wipLimit"]; ok {
				limit, valid := asInt(value)
				if !valid || limit < 0 || limit > 10000 {
					return nil, nil, BoardConfig{}, domain.Invalid("invalid column WIP limit")
				}
			}
		}
	}
	return doc, nc, cfg, nil
}

// GetBoard resolves the full board view: configured columns with ordered
// cards plus an overflow bucket so unknown folders never hide tasks.
func (s *Services) GetBoard(ctx context.Context, a *Actor, spaceID, boardPath string) (*Board, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(boardPath)
	if perr != nil {
		return nil, perr
	}
	_, nc, cfg, err := s.loadBoardConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	docs, err := s.ListDocuments(ctx, a, spaceID, false)
	if err != nil {
		return nil, err
	}
	byColumn := map[string][]BoardCard{}
	var overflow []BoardCard
	boardMD := boardMDPath(canon)
	assetsPrefix := canon + "/" + boardAssetsName + "/"
	for _, d := range docs {
		if d.TombstoneAt != nil {
			continue
		}
		if !underPrefix(d.Path, canon) {
			continue
		}
		if d.Path == boardMD {
			continue
		}
		if !isBoardCardKind(d.Kind) {
			continue
		}
		if strings.HasPrefix(d.Path, assetsPrefix) {
			continue
		}
		card := BoardCard{ID: d.ID, Path: d.Path, Title: d.Title, Revision: d.CurrentRevision, UpdatedAt: d.UpdatedAt}
		colID := boardColumnForPath(cfg, canon, d.Path)
		if colID == "" {
			overflow = append(overflow, card)
			continue
		}
		byColumn[colID] = append(byColumn[colID], card)
	}
	sortCards(overflow)
	columns := make([]BoardColumnView, 0, len(cfg.Columns))
	for _, c := range cfg.Columns {
		cards := byColumn[c.ID]
		if cards == nil {
			cards = []BoardCard{}
		}
		applyOrder(cards, cfg.Order[c.ID])
		columns = append(columns, BoardColumnView{BoardColumn: c, Cards: cards})
	}
	var warnings []string
	if store, err := folderStore(s); err == nil {
		folders, err := store.Folders(spaceID)
		if err != nil {
			return nil, err
		}
		present := map[string]bool{}
		for _, f := range folders {
			present[f] = true
		}
		for _, c := range cfg.Columns {
			if !present[canon+"/"+c.Folder] {
				warnings = append(warnings, "Column folder is missing: "+c.Folder+". Save board settings to recreate it.")
			}
		}
	}
	title := cfg.Title
	if title == "" {
		title = domain.BaseName(canon)
	}
	return &Board{
		Path: canon, Title: title, Revision: nc.Revision, Config: cfg,
		Columns: columns, Overflow: overflow, Warnings: warnings,
	}, nil
}

func sortCards(cards []BoardCard) {
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Path != cards[j].Path {
			return cards[i].Path < cards[j].Path
		}
		return cards[i].ID < cards[j].ID
	})
}

// applyOrder reorders cards by the stored id sequence, appending any card not
// listed (stale ids are ignored) sorted by path so nothing disappears.
func applyOrder(cards []BoardCard, order []string) {
	if len(order) == 0 {
		sortCards(cards)
		return
	}
	index := map[string]int{}
	for i, id := range order {
		index[id] = i
	}
	sort.SliceStable(cards, func(i, j int) bool {
		oi, okI := index[cards[i].ID]
		oj, okJ := index[cards[j].ID]
		if okI && okJ {
			return oi < oj
		}
		if okI {
			return true
		}
		if okJ {
			return false
		}
		if cards[i].Path != cards[j].Path {
			return cards[i].Path < cards[j].Path
		}
		return cards[i].ID < cards[j].ID
	})
}

// ListBoards discovers every folder that carries a board.md configuration.
func (s *Services) ListBoards(ctx context.Context, a *Actor, spaceID string) ([]BoardSummary, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	docs, err := s.ListDocuments(ctx, a, spaceID, false)
	if err != nil {
		return nil, err
	}
	out := []BoardSummary{}
	for _, d := range docs {
		if d.TombstoneAt != nil || d.Kind != "note" {
			continue
		}
		if !strings.HasSuffix(d.Path, "/"+boardFileName) {
			continue
		}
		boardPath := strings.TrimSuffix(d.Path, "/"+boardFileName)
		nc, err := s.ReadNote(ctx, a, spaceID, d.ID)
		if err != nil {
			continue
		}
		fm, _, _ := markdown.ParseFrontmatter(nc.Markdown)
		cfg, ok := parseBoardConfig(fm)
		if !ok {
			continue
		}
		title := cfg.Title
		if title == "" {
			title = domain.BaseName(boardPath)
		}
		out = append(out, BoardSummary{Path: boardPath, Title: title, Columns: len(cfg.Columns)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// CreateBoard creates a board folder with its configuration and column
// folders. When templatePath is given the template's column configuration and
// creation defaults are copied, but card ordering (task membership) is cleared.
func (s *Services) CreateBoard(ctx context.Context, a *Actor, spaceID, boardPath string, columns []BoardColumn, title, templatePath, idemKey string) (*Board, error) {
	boardLock := s.docLock("kanban:" + spaceID)
	boardLock.Lock()
	defer boardLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionCreate); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(boardPath)
	if perr != nil {
		return nil, perr
	}
	if err := validFolderRel(canon); err != nil {
		return nil, err
	}
	if canon == "" {
		return nil, domain.Invalid("board folder must not be the vault root")
	}

	boards, listErr := s.ListBoards(ctx, a, spaceID)
	if listErr != nil {
		return nil, listErr
	}
	for _, board := range boards {
		if board.Path != canon && (underPrefix(canon, board.Path) || underPrefix(board.Path, canon)) {
			return nil, domain.Invalid("owning boards cannot be nested; use a sibling board folder")
		}
	}
	cfg := BoardConfig{Version: 1, Title: strings.TrimSpace(title)}
	if templatePath != "" {
		tcanon, _, tperr := domain.NormalizePath(templatePath)
		if tperr != nil {
			return nil, tperr
		}
		_, _, tcfg, err := s.loadBoardConfig(ctx, a, spaceID, tcanon)
		if err != nil {
			return nil, domain.NotFound("template board not found")
		}
		cfg.Columns = copyColumns(tcfg.Columns)
		if tcfg.CreateDefaults != nil {
			cfg.CreateDefaults = &BoardCreateDefaults{Column: tcfg.CreateDefaults.Column, Content: tcfg.CreateDefaults.Content}
		}
		// Templates copy configuration without task membership: order is cleared.
		cfg.Order = nil
	} else {
		if len(columns) > 0 {
			cfg.Columns = copyColumns(columns)
		} else {
			cfg.Columns = defaultBoardColumns()
		}
	}
	if err := validateBoardConfig(cfg); err != nil {
		return nil, err
	}

	body := "# " + cfg.Title + "\n\nThis file configures the board. Manage columns and cards from the board view.\n"
	if cfg.Title == "" {
		body = "# Board\n\nThis file configures the board. Manage columns and cards from the board view.\n"
	}
	content, err := renderBoardMarkdown(cfg, body)
	if err != nil {
		return nil, err
	}
	if _, err := s.CreateNote(ctx, a, spaceID, boardMDPath(canon), content, idemKey, false); err != nil {
		return nil, err
	}
	// Folder failures are explicit; idempotent replay can repair a partially
	// initialized board without duplicating the configuration document.
	for _, c := range cfg.Columns {
		if err := s.CreateFolder(ctx, a, spaceID, canon+"/"+c.Folder); err != nil {
			return nil, domain.Unavailable("board configuration saved, but column folder creation failed; retry to repair")
		}
	}
	if err := s.CreateFolder(ctx, a, spaceID, canon+"/"+boardAssetsName); err != nil {
		return nil, domain.Unavailable("board configuration saved, but assets folder creation failed; retry to repair")
	}
	return s.GetBoard(ctx, a, spaceID, canon)
}

func copyColumns(in []BoardColumn) []BoardColumn {
	out := make([]BoardColumn, len(in))
	copy(out, in)
	return out
}

func normKeyOr(path string) string {
	_, key, err := domain.NormalizePath(path)
	if err != nil {
		return ""
	}
	return key
}

// UpdateBoardConfigRequest carries a new configuration plus explicit
// relocation instructions for columns that are being removed.
type UpdateBoardConfigRequest struct {
	Columns        []BoardColumn        `json:"columns"`
	Title          string               `json:"title,omitempty"`
	CreateDefaults *BoardCreateDefaults `json:"createDefaults,omitempty"`
}

// UpdateBoardConfig saves settings without moving documents. Populated
// column removal/folder changes are refused until users move their files.
func (s *Services) UpdateBoardConfig(ctx context.Context, a *Actor, spaceID, boardPath string, req UpdateBoardConfigRequest, expectedRevision string) (*Board, error) {
	boardLock := s.docLock("kanban:" + spaceID)
	boardLock.Lock()
	defer boardLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if expectedRevision == "" {
		return nil, domain.PreconditionRequired("If-Match with the current board revision is required")
	}
	canon, _, perr := domain.NormalizePath(boardPath)
	if perr != nil {
		return nil, perr
	}
	doc, nc, oldCfg, err := s.loadBoardConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	if nc.Revision != expectedRevision {
		return nil, domain.PreconditionFailed(nc.Revision)
	}

	newCfg := BoardConfig{
		Version:        oldCfg.Version,
		Title:          strings.TrimSpace(req.Title),
		Columns:        copyColumns(req.Columns),
		CreateDefaults: req.CreateDefaults,
		Order:          oldCfg.Order,
	}
	if newCfg.Title == "" {
		newCfg.Title = oldCfg.Title
	}
	if len(newCfg.Columns) == 0 {
		newCfg.Columns = copyColumns(oldCfg.Columns)
	}
	if req.CreateDefaults == nil {
		newCfg.CreateDefaults = oldCfg.CreateDefaults
	}
	for key := range newCfg.Order {
		if _, exists := findColumn(newCfg.Columns, key); !exists {
			delete(newCfg.Order, key)
		}
	}
	if err := validateBoardConfig(newCfg); err != nil {
		return nil, err
	}

	docs, err := s.ListDocuments(ctx, a, spaceID, false)
	if err != nil {
		return nil, err
	}
	for _, old := range oldCfg.Columns {
		next, kept := findColumn(newCfg.Columns, old.ID)
		if kept && next.Folder == old.Folder {
			continue
		}
		for _, d := range docs {
			if strings.HasPrefix(d.Path, canon+"/"+old.Folder+"/") {
				return nil, domain.Conflict("move documents out of column " + old.Label + " before removing or changing its folder")
			}
		}
		for _, col := range newCfg.Columns {
			if col.ID != old.ID && strings.EqualFold(col.Folder, old.Folder) {
				return nil, domain.Invalid("a column folder cannot be reassigned to a different column id")
			}
		}
	}
	for _, col := range newCfg.Columns {
		if err := s.CreateFolder(ctx, a, spaceID, canon+"/"+col.Folder); err != nil {
			return nil, err
		}
	}

	// Preserve the board.md body; only the configuration frontmatter changes.
	content, err := renderBoardPreserving(nc.Markdown, newCfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveContent(ctx, a, spaceID, doc.ID, expectedRevision, content, "", shaHex([]byte(nc.Markdown))); err != nil {
		return nil, err
	}
	return s.GetBoard(ctx, a, spaceID, canon)
}

// CreateTask creates a task document inside a board column folder.
func (s *Services) CreateTask(ctx context.Context, a *Actor, spaceID, boardPath, columnID, title, content, idemKey string) (*BoardCard, error) {
	boardLock := s.docLock("kanban:" + spaceID)
	boardLock.Lock()
	defer boardLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionCreate); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(boardPath)
	if perr != nil {
		return nil, perr
	}
	_, _, cfg, err := s.loadBoardConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	if columnID == "" {
		columnID = "todo"
		if cfg.CreateDefaults != nil && cfg.CreateDefaults.Column != "" {
			columnID = cfg.CreateDefaults.Column
		}
	}
	col, ok := findColumn(cfg.Columns, columnID)
	if !ok {
		return nil, domain.Invalid("unknown column: " + columnID)
	}
	name := strings.TrimSpace(title)
	if name == "" {
		return nil, domain.Invalid("task title must not be empty")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	safe, _, nameErr := domain.NormalizePath(name)
	if nameErr != nil {
		return nil, nameErr
	}
	if safe != name || strings.Contains(name, "/") || strings.HasPrefix(name, ".") {
		return nil, domain.Invalid("task title must be a file name without folders or leading dots")
	}
	path := canon + "/" + col.Folder + "/" + name
	if content == "" && cfg.CreateDefaults != nil {
		content = cfg.CreateDefaults.Content
	}
	if content == "" {
		content = "# " + strings.TrimSuffix(name, ".md") + "\n\n"
	}
	meta, err := s.CreateNote(ctx, a, spaceID, path, content, idemKey, false)
	if err != nil {
		return nil, err
	}
	return &BoardCard{ID: meta.ID, Path: meta.Path, Title: meta.Title, Revision: meta.CurrentRevision, UpdatedAt: meta.UpdatedAt}, nil
}

// MoveTask relocates a task document into a board column folder, preserving
// its document id and history. Status is derived from the new location.
func (s *Services) MoveTask(ctx context.Context, a *Actor, spaceID, boardPath, docID, columnID, expectedRevision string, expectedPaths ...string) (*BoardCard, error) {
	boardLock := s.docLock("kanban:" + spaceID)
	boardLock.Lock()
	defer boardLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if expectedRevision == "" {
		return nil, domain.PreconditionRequired("If-Match with the current task revision is required")
	}
	canon, _, perr := domain.NormalizePath(boardPath)
	if perr != nil {
		return nil, perr
	}
	_, _, cfg, err := s.loadBoardConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	col, ok := findColumn(cfg.Columns, columnID)
	if !ok {
		return nil, domain.Invalid("unknown column")
	}
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt != nil {
		return nil, domain.NotFound("task not found")
	}
	if !isBoardCardKind(doc.Kind) {
		return nil, domain.Invalid("only Markdown documents can be tasks")
	}
	source, err := s.ReadNote(ctx, a, spaceID, doc.ID)
	if err != nil {
		return nil, err
	}
	fm, _, _ := markdown.ParseFrontmatter(source.Markdown)
	if _, isBoard := parseBoardConfig(fm); isBoard && strings.EqualFold(path.Base(doc.Path), boardFileName) {
		return nil, domain.Invalid("board configuration is not a task")
	}
	from := doc.Path
	if len(expectedPaths) > 0 && expectedPaths[0] != from {
		return nil, domain.Conflict("task has moved; reload the board")
	}
	if source.Revision != expectedRevision {
		return nil, domain.PreconditionFailed(source.Revision)
	}
	next := canon + "/" + col.Folder + "/" + path.Base(from)
	warnings, err := s.moveBoardDocument(ctx, a, spaceID, doc, source, next, expectedRevision)
	if err != nil {
		return nil, err
	}
	updated, err := s.ReadNote(ctx, a, spaceID, doc.ID)
	if err != nil {
		return nil, err
	}
	return &BoardCard{ID: doc.ID, Path: updated.Path, Title: updated.Title, Revision: updated.Revision, UpdatedAt: time.Now().UTC(), Warnings: warnings}, nil
}

// ReorderCards stores a new card order for one column. Cards not listed are
// appended; stale ids are ignored. Guarded by If-Match on the board revision.
func (s *Services) ReorderCards(ctx context.Context, a *Actor, spaceID, boardPath, columnID string, orderedIDs []string, expectedRevision string) (*Board, error) {
	boardLock := s.docLock("kanban:" + spaceID)
	boardLock.Lock()
	defer boardLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if expectedRevision == "" {
		return nil, domain.PreconditionRequired("If-Match with the current board revision is required")
	}
	canon, _, perr := domain.NormalizePath(boardPath)
	if perr != nil {
		return nil, perr
	}
	doc, nc, cfg, err := s.loadBoardConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	if nc.Revision != expectedRevision {
		return nil, domain.PreconditionFailed(nc.Revision)
	}
	col, ok := findColumn(cfg.Columns, columnID)
	if !ok {
		return nil, domain.Invalid("unknown column: " + columnID)
	}
	board, err := s.GetBoard(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	var current []BoardCard
	for _, c := range board.Columns {
		if c.ID == col.ID {
			current = c.Cards
			break
		}
	}
	inColumn := map[string]bool{}
	for _, c := range current {
		inColumn[c.ID] = true
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(current))
	for _, id := range orderedIDs {
		if inColumn[id] && !seen[id] {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	for _, c := range current {
		if !seen[c.ID] {
			ordered = append(ordered, c.ID)
		}
	}
	if cfg.Order == nil {
		cfg.Order = map[string][]string{}
	}
	if len(ordered) == 0 {
		delete(cfg.Order, col.ID)
	} else {
		cfg.Order[col.ID] = ordered
	}
	content, err := renderBoardPreserving(nc.Markdown, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveContent(ctx, a, spaceID, doc.ID, expectedRevision, content, "", shaHex([]byte(nc.Markdown))); err != nil {
		return nil, err
	}
	return s.GetBoard(ctx, a, spaceID, canon)
}

func findColumn(columns []BoardColumn, id string) (BoardColumn, bool) {
	for _, c := range columns {
		if c.ID == id {
			return c, true
		}
	}
	return BoardColumn{}, false
}
