# Mind maps

Mind maps give ordinary vault documents a spatial view. Open **Maps** in the
left ribbon, choose **Create map**, and add your first idea. Each map is a
folder containing `mindmap.md`; its canvas starts blank. The Maps list uses compact, keyboard-accessible tiles: click a tile or focus it and press Enter/Space to open it. A map icon identifies the tool, while the node and connector counts describe its contents; no invented topology is shown. Existing folder names are preserved. The list groups maps by their parent folder and offers search and a folder filter; each tile shows the latest document activity inside its folder. The file explorer's **⊕** menu can also create a map in a chosen folder, and map folders and their `mindmap.md` files carry an **Open map** quick button.

The canvas uses a quiet dotted background that follows pan and zoom. **Arrange branches** has its own branching-layout icon, distinct from the graph view.

## Working on a canvas

- Add ideas with **Add node** or by double-clicking empty space. Each new idea
  creates an ordinary Markdown note in the map folder.
- Single-click a card header/frame to select it. Double-click the header/frame,
  or press Space on a focused card, to open its editable side preview. Selecting
  another card does not switch the preview. Enter or **Open document** in the
  card menu opens the full document tab.
- Opening, switching, resizing or closing the preview preserves canvas zoom.
  Only an obscured previewed card causes a minimal pan into the uncovered
  canvas; already-visible cards leave the camera untouched. Oversized cards
  keep their header visible without zooming out. Fit is an explicit action.
- Drag cards to arrange them. Scroll or pinch to zoom, drag empty space to pan,
  and use the floating navigation panel at the bottom-right to zoom in/out,
  reset to 100%, or **Fit canvas to content**. Controls stay visible beside an
  open document preview and work in read-only maps. Use **Show minimap** for
  click/drag navigation and a visible viewport outline. See [canvas navigation](canvas-navigation.md).
- Press Tab on a focused idea to create a connected child, or Shift+Tab for a
  sibling. Arrows navigate ideas; Alt+arrows move one by keyboard. Escape leaves
  the canvas interaction. Ordinary Tab navigation remains available in controls.
- Hover or focus a card to reveal connection points. Drag a point to another
  card, or release on empty canvas to create a connected idea there. Click a
  point then a destination for click-to-connect; Escape cancels. Starting or
  canceling a connection never shifts, refits or pans the canvas.
- Resize a card with its bottom-right diagonal corner grip (arrow keys also
  work, and the grip stays live while the card's text is being edited inline).
  Expanded cards show a rendered Markdown mini preview; linked files appear as
  title chips. Editing a node's document in the preview updates its mini
  preview promptly.
- Use **Connect selected idea**, then select the destination idea. The node's
  ellipsis menu also provides this action. Select a connector by clicking its
  line (the hit area stays generous at any zoom) or by pressing Tab to reach
  the connector — each line is a focusable control labelled with its two
  ideas — and pressing Enter or Space, to edit its label, make it dashed, or
  remove it. Drag a selected connector's
  endpoint handles, or use **Move start**/**Move end**, to reconnect it to
  another idea while keeping its label; Escape cancels. Delete/Backspace
  removes only the selected connector, never its linked documents.
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


### Edit text on the map

Double-click a card's rendered body, click its pencil button, or focus the card and press **F2**. Type directly inside the card: the inline editor has no formatting toolbar and no Done button, and the card keeps its saved size while editing — the editor simply fills the card body's space with the same typography as the rendered preview, so nothing jumps. **Escape** or **Ctrl/Cmd+Enter** saves and returns to the card; clicking outside the editor also finishes editing and saves the latest text (the link picker, save-error controls and the resize grip never close it early). The bottom-right corner grip keeps resizing the card during editing, by drag or arrow keys, without closing the editor or losing the caret. A save failure keeps your draft and editor available with retry, resolve and export. The same Markdown file and shared autosave draft are used by the document tab and side preview. To edit raw Markdown, double-click the card header to open its preview and use **Source** there.

Dragging a node or connection near an edge automatically pans the canvas, faster as the pointer approaches the edge. Keep the pointer near the edge to continue; release to finish, or press **Escape** to cancel the move. Zoom and minimap navigation remain available in the navigation panel.

Map cards use 16px titles and 15px Markdown body text before canvas zoom, with
matching inline-editor typography. Resizing a card reveals more content without
changing its document or the camera scale.

The preview gesture, inline editing and initial minimap visibility are adjustable
in [Settings → Interaction](interaction-preferences.md). The descriptions above
use the default gestures.
