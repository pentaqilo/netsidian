# Mind maps

Mind maps give ordinary vault documents a spatial view. Open **Maps** in the
left ribbon, choose **Create mind map**, and add your first idea. Each map is a
folder containing `mindmap.md`; its canvas starts blank.

## Working on a canvas

- Add ideas with **Add node** or by double-clicking empty space. Each new idea
  creates an ordinary Markdown note in the map folder.
- Select an idea to edit its document in the resizable Visual Markdown preview.
  Double-click, or press Enter on a focused idea, to open its document tab.
- Drag cards to arrange them. Scroll or pinch to zoom, drag empty space to pan,
  and use **Fit canvas to content** to return to your ideas.
- Press Tab on a focused idea to create a connected child, or Shift+Tab for a
  sibling. Arrows navigate ideas; Alt+arrows move one by keyboard. Escape leaves
  the canvas interaction. Ordinary Tab navigation remains available in controls.
- Hover or focus a card to reveal connection points. Drag a point to another
  card, or release on empty canvas to create a connected idea there. Click a
  point then a destination for click-to-connect; Escape cancels.
- Resize a card with its bottom-right grip (arrow keys also work). Expanded
  cards show a rendered Markdown mini preview; linked files appear as title chips.
- Use **Connect selected idea**, then select the destination idea. The node's
  ellipsis menu also provides this action. Select a connector on the canvas or
  through **Connections** to edit its label, make it dashed, or remove it.
- **Arrange branches** spaces rows and levels using the current card dimensions. Undo/redo restores
  canvas edits, including arrangement, sizes, colors and connectors. It does not undo
  document edits or physical file operations; those use document history/trash.
- Node actions appear on hover and keyboard focus. They include color, open,
  copy path, reference-to-owned move, file collection, remove, and explicit trash.

## Existing documents and file collections

Drop a document or attachment from Files onto empty canvas to create a new
Markdown card linking that file. Drop it onto an existing card to add a normal
Markdown link there. **The original file stays in place.** Click a linked-file
chip to open it. A card's content and linked files are shared wherever its
Markdown document is used.

**Link an existing document** is a separate shortcut to display the original
Markdown document itself as a card. A document may appear on several maps;
positions, dimensions, colors, connector sides and visual connectors belong
only to the current map.

Physical collection remains an explicit **Move another file into this node**
action, with a confirmation showing source and destination. Its destination is
`<map>/Nodes/<node-document-id>/<filename>`. Existing collected files are
preserved. Collection is never the default drop behavior.

An outside reference can also be explicitly moved into the map's root folder.
Removing an idea from the canvas keeps its document and collected files.
**Move document to trash** is separate and only applies to a map-owned document;
it does not trash its collected folder. Referenced documents are never trashed
by removing their nodes. Folder-wide deletion remains an explicit Files action.

## Persistence and integration

`mindmap.md` stores versioned YAML: document IDs, positions, optional dimensions
and colors, and directed connectors with optional border attachment sides, labels and dashed style. Document
IDs survive renames and moves within the vault. Missing or trashed documents
remain visible as missing nodes; restoring their documents resolves them again.
The map folder can be renamed or moved through the existing Files workflow.

Canvas edits autosave with a revision precondition. New edits made during a save
are kept and saved afterward. Failed saves remain in local recovery storage,
scoped by user, vault and map. Closing map tabs and moving their folders flush pending canvas edits first.
A conflicting remote edit requires an explicit
choice to load the server layout or save the recovered layout over the latest
server revision. Neither option silently merges two layouts. Read-only vaults
can browse maps and documents but cannot mutate them.

Map membership is also represented by ordinary Markdown links in a managed
section of `mindmap.md`. These participate in backlinks and the knowledge graph.
Dropping or collecting a file adds a normal link to its node document. Canvas connectors are
map-specific visual relationships; they do not silently change the prose of
referenced documents. Nodes may use normal document links, tags and memo tags;
existing editors, search, Memos, graph and revisions continue to work.

## Initial limits

- Up to 200 document nodes and 400 connectors per map. Canvas configuration
  and Markdown remain ordinary versioned documents; no new database schema.
- Every node has a Markdown backing file. Documents and binary attachments can
  be linked by dropping them onto cards or the canvas. Folder drops are excluded.
- Layouts are free-form directed diagrams; there is no real-time collaborative
  cursor, multi-selection/group movement, or drawing tool yet.
- Backup/restore preserves document IDs and canvas membership. Copying only the
  Markdown files into a separately indexed vault assigns new IDs; node references
  then need relinking. A portable path-based map importer is not included yet.
- Recovery is local to the browser. Cross-device work uses committed server state.
- Link repair or collection-link creation can partially fail after a successful
  file move; the UI reports warnings rather than overwriting concurrent edits.

## Review checklist

Create a blank map; create an idea and two branches; drag, connect and label ideas;
undo and arrange; drop a file onto the canvas and onto a card without moving it;
resize cards; drag connectors between borders and onto empty canvas; edit
in the preview; reload and reopen tabs; inspect the normal files and graph.
Verify that removing a reference keeps the source note. Test narrow and desktop
layouts, themes, keyboard navigation, and a read-only vault before approval.

[Desktop preview](screenshots/mindmap.png) · [Light theme](screenshots/mindmap-light.png) · [Phone layout](screenshots/mindmap-mobile.png)

## File model decision

The board is one metadata document, `mindmap.md`; its enclosing folder organizes
new Markdown cards, similar to Kanban. It does not duplicate card bodies.
Each node is ordinary Markdown containing its content and semantic file links.
The view resolves nodes against the vault on load. Visual connections remain
in board metadata, rather than being written into every referenced note: this
avoids conflicting layouts and multi-file partial saves when a note appears
on multiple boards. A drawn arrow does not automatically become a global
semantic relationship. Add a normal Markdown link for that relationship.
Folder-backed visual groups are deferred; no automatic file moves are introduced.
See [interaction decisions and research](mindmap-ux.md) for the tradeoffs.
