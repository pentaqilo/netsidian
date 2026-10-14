# Folder-backed Kanban boards

A board is an ordinary vault folder containing a `board.md` configuration
and status subfolders. Tasks are ordinary Markdown documents. Their physical
location determines membership and status; no second task status is stored in
frontmatter or the board configuration.

```text
Tasks/Website launch/
  board.md
  todo/Review landing page.md
  in-progress/Fix login redirect.md
  done/Configure domain.md
  assets/
```

Create a board with To do, In progress and Done, or use another board as a
configuration template. Templates copy columns and creation defaults, without
copying task files or card ordering. Click a task once (or press Space) to open
its Visual Markdown Editor beside the board. Double-click or press Enter to
open the normal document tab. Task details sit at the top, with the close
button in the top-right corner.

Drag the pane’s left edge to resize it, or focus the separator and use arrow
keys. Narrow screens use a bottom pane with a top resize handle. Size preferences
are retained for the current account, vault and board. Common formatting tools stay
visible; More formatting opens the remaining tools without hiding the document.

Edits autosave through the same draft controller as the document tab. Closing
the preview or selecting another task saves first; if a save fails or conflicts,
the draft remains available for retry, comparison or export. Viewers can read
but cannot edit. Unsupported Markdown uses the editor’s existing source fallback
so protected metadata, links and other vault syntax are preserved.

Task status moves preserve document identity. Preview content and task details
follow saved changes; removing a task clears a clean preview, while unsaved
content remains recoverable. Tags, checklists, links, Memos, graph indexing and
revision history continue to use the underlying document.

`board.md` uses YAML frontmatter with `netsidian: board`, `version: 1`, and
`columns`. Each column has a stable `id`, display `label`, single-segment `folder`,
optional `completion` flag and nonnegative `wipLimit`. `order` maps column IDs to
ordered document IDs; missing/stale entries never suppress real tasks. Optional
`createDefaults` contains `column` and Markdown `content`. Board settings edits
preserve explanatory Markdown and unrelated frontmatter.

An existing document moved to a board changes its file location. A memo keeps
its memo tag and remains in the timeline, though folder-filtered views change.
Each task has one owning board. Boards cannot be nested; multiple-board reference
cards and cross-vault moves are outside the first release. Documents outside
configured columns appear separately, while board-level assets do not become
cards.

Column label changes do not move files. Populated columns cannot be removed or
assigned a different folder through settings: move their documents first.
Malformed/future-version configuration refuses edits rather than overwriting
unknown data. Missing configured folders are reported and can be recreated by
saving settings. File-manager changes rely on the vault's existing import and
reconciliation behavior; this feature does not introduce filesystem watching.

Board task moves use the existing journaled rename with source-path and content
revision checks, preserving document identity, tabs and history. Supported local
Markdown links, reference definitions, wikilinks and attachment links are repaired;
code examples, frontmatter, comments, unresolved references and external URLs stay
unchanged. Link repairs across multiple documents are revision-checked, not one
cross-file transaction. If an overlapping edit prevents a repair, the move remains
completed and returns a warning identifying the document needing attention. No
concurrent content is overwritten. Arbitrary HTML links and other unsupported
Markdown extensions are not automatically rewritten.

Board settings and ordering use the `board.md` revision (`If-Match`); task moves
also require the expected source path. Reload after a conflict before retrying.
Normal vault permissions and PAT document scopes apply. Moving a task to Done
retains its document. Trashing a task uses existing recoverable document trash;
trashing the owning board folder includes its contained documents.
