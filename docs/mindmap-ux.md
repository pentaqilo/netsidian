# Mind map interaction decisions

The canvas combines mind mapping with editable Markdown cards. The principles below guide its first review revision.

## Connect and navigate

- Show connection points on hover and keyboard focus, with larger hit areas than their visible dots. Support dragging a point and click-to-connect.
- Highlight valid targets. A temporary connector follows the pointer; Escape or pointer cancellation leaves no permanent connection.
- Releasing on empty canvas offers a new node at that location. Canceling must create neither a file nor a dangling connector.
- Connection state must never change the canvas bounding box: no banner or hint label is inserted into the layout, and automatic fit runs only for initial map layout; later container changes preserve zoom, so starting, dragging, releasing or canceling a connection never shakes, refits or pans the board.
- Existing connectors are first-class objects: a zoom-independent invisible hit path along the curve selects them, and the same path is a keyboard-focusable control labelled with its two ideas (Tab reaches it, Enter or Space opens the inspector, Delete removes it). There is no separate Connections dropdown. The selection is visually distinct, and the inspector offers label editing, deletion of the connection only (never its linked documents), and endpoint reconnection that preserves the connector's identity and label. Escape cancels a reconnect without saving. Delete/Backspace removes the selected connector only while focus is on the canvas or connection controls, outside text inputs; read-only viewers can select and inspect but not mutate.
- Attach connectors to card borders and align Bezier tangents with border normals. Moving or resizing cards recalculates geometry.
- Pan and zoom apply to both cards and the dotted background. Account for the application's independent UI scale when translating screen coordinates.
- Keep moving a card distinct from interacting with its body, links, resize grip, and connector handles.

These decisions adapt [Miro mind map interactions](https://help.miro.com/hc/en-us/articles/360017730753-Mind-map), React Flow's [connection-on-drop pattern](https://reactflow.dev/examples/nodes/add-node-on-edge-drop), [floating edges](https://reactflow.dev/examples/edges/floating-edges), and [node resizing](https://reactflow.dev/examples/nodes/node-resizer). The application uses its existing canvas rather than introducing a second diagramming framework.

## Compact header and card movement

- The title sits above one compact location/action row: folder path and
  Configuration on the left, add/link/undo/redo/arrange/connect/reload on the
  right. Narrow screens use compact icons and wrap without page overflow.
- There is no ordinary Saved/Saving chip. Autosave is quiet; only real
  conflict or failure states appear, as banners with retry, save-over, and
  load-theirs recovery actions. Persistence itself is unchanged.
- A card's header/frame (title, path, padding) is the defined drag surface;
  the selectable body, links, chips, ports and the resize grip keep their own
  gestures. A frame press suppresses native text dragging; a stale DOM text
  selection is cleared only after the move threshold is crossed, so a simple
  click preserves the selection. Canceled pointer drags restore the position. Selecting and copying body
  text, editing the preview, and read-only behavior are unaffected.

## Files and ownership

A drop is a reference operation: dropping onto a card links the source file; dropping onto empty canvas creates a Markdown card linking it. The original file stays in place. Links appear as recognizable file chips with titles, and normal Markdown links participate in existing navigation and backlinks. Explicit file moves remain separate actions.

A board metadata file owns membership, placement, size, color, and visual connector presentation. Markdown owns card content and ordinary semantic file links. Putting board layout into shared Markdown would cause one board's arrangement to change another board unexpectedly. Conversely, duplicating the same relationship independently across several files introduces partial-save and stale-data problems. Any node connection metadata must distinguish shared semantic relationships from board-specific visual connections and declare one authoritative source.

Editing a node's document in the side preview must refresh that node's mini-card body promptly — while editing and after the preview moves to another node — using the shared draft's saved content and the existing safe Markdown renderer, without saving board metadata and without stale responses overwriting newer text.

The [JSON Canvas specification](https://jsoncanvas.org/spec/1.0/) provides a useful precedent for file references, dimensions, and connector attachment sides. Netsidian retains Markdown rather than switching its existing format to JSON Canvas.

Folders can organize files or future groups. Merely adding a card to a group should not silently move its backing file. Existing maps and their collected files must remain readable; metadata evolution must preserve human-written content and foreign frontmatter.

## Compact Memos

Keep the writing area and recent thoughts prominent. Secondary filters belong in a sidebar on the left on wide screens and in the existing filter dialog on narrow screens; the sidebar width is user-resizable through a keyboard-accessible separator (clamped and persisted per user and vault) without disturbing date selection or causing horizontal overflow. Saving remains explicit and aligned to the right. The required `memo` classification is automatic and no tag editor sits above the composer; tags stay part of the stored document and the timeline cards. The composer panel itself grows with the draft instead of giving the text box an inner scroll. An obvious Visual/Markdown toggle exposes the existing WYSIWYG editor for memos; both modes edit the same Markdown draft, toggling never submits or loses content, and draft autosave, explicit save, shortcuts and image handling behave identically in either mode. The composer defaults to the shared compact Visual editor: hashtags receive live inline decorations, and ordinary Markdown formatting renders as it is typed. Decorations never change the stored text or selection, and ignore code and link text. Focus/blur never swaps the writing surface, changes its layout, or creates a second preview. Linking guidance is an explicit disclosure instead of a focus-triggered callout; the compact mobile toolbar and two-row footer keep formatting, the mode switch and right-aligned Save accessible. Markdown mode is an explicit source-editing choice and remains raw while focused or blurred. Default memo styling follows the main app accent while retaining explicit customization.


## Edge navigation and direct editing

Dragging a node, creating a connector or moving its endpoint near a canvas edge pans the camera in that direction. A 48 px activation band ramps up to 650 px/second, with elapsed-time animation so high refresh rate displays behave consistently. Pointer coordinates account for both canvas zoom and app zoom. A stationary pointer keeps panning; the dragged card or connector endpoint remains anchored under it. Floating navigation, minimaps and the document preview do not act as drop targets or scroll zones. Hovering, selecting text, resizing and manual background panning never start auto-pan.

Releasing stops edge navigation. Escape, pointer cancellation or unexpected loss of capture cancels the gesture without saving an intermediate node position; the current camera position is retained. Hidden workspaces and hidden browser tabs stop the animation. Panning alone does not change the map file.

Double-click the rendered body, use the pencil button, or press F2 on a focused node to edit its Markdown directly. The title/frame remains the drag surface: single click selects, double-click opens the side preview, Space previews, and Enter/menu explicitly opens the full document. One active inline Visual editor uses the same autosaving document draft as tabs and the side preview. The side editor pauses while inline editing is active. The inline editor is the bare editing surface: no formatting toolbar and no Done button — the card frame itself stays the control surface, and Escape or Ctrl/Cmd+Enter remain the finish shortcuts. Losing focus outside the editor finishes editing and saves the latest text; focus moving into the editor's own error/retry controls, the document-link picker portal it owns, or the card's resize grip does not close it. The card keeps its saved dimensions while editing — no temporary expansion, no entry zoom or recentering — and the editor body occupies the same slot, padding and typography as the rendered card body, preserving linked-file chips and the scroll position, so entering and leaving editing never reflows the canvas, scrolls the page or moves the camera; the body simply takes the card's available space. Small cards show their body in both modes and omit the path line in both modes. The bottom-right corner grip is a real diagonal resize handle and stays live during editing (pointer and keyboard): pressing it does not steal the caret or text selection, the drag starts from the current saved size with no snap-back, pointer events do not propagate to canvas gestures, and the edit remains active. Failed saves and conflicts retain the editor and expose retry, resolve and export actions. An async finish never closes a newer edit, and an outside click never steals focus back to the node. The editor owns text selection, scrolling and keyboard shortcuts. On phones the side pane temporarily yields to the card. Existing frontmatter and protected vault links retain the Visual editor's normal safeguards; raw Markdown stays available through the side preview's Source mode.

These choices adapt [React Flow's auto-pan on node drag and connect](https://reactflow.dev/api-reference/react-flow), [tldraw's proximity-based edge scrolling](https://tldraw.dev/examples/edge-scrolling), and [Miro's double-click to edit mind-map text](https://help.miro.com/hc/en-us/articles/360017730753-Mind-map). Explicit edit entry avoids accidentally replacing a file while navigating the canvas; keeping the card at its saved size with the editor in the body's existing slot keeps small cards usable without expansion, reflow or camera moves.


## Preview on demand, without changing zoom

A single header/frame click selects an idea for canvas actions and never opens or switches its document preview. A header/frame double-click or Space opens the editable preview. Body double-click still edits inline. Enter and the explicit menu action open the full document. Connection completion, read-only inspection and unsaved-draft guards retain their existing behavior.

The canvas fits once on initial layout. Opening, switching, closing or resizing the preview never auto-fits or changes zoom; window resizing also preserves the current zoom. After the preview lays out, only its requested node is revealed with the smallest translation needed in each axis. Geometry uses the actual uncovered canvas and independent application UI scale, including the phone preview. Already-visible cards leave the camera unchanged. For a card too large to fit at the current zoom, its leading header stays accessible instead of alternating edge alignment or zooming out. Preview closure retains the camera. Fit and Arrange remain explicit ways to reframe the canvas. These camera changes never save node positions or map metadata.


## Quiet Maps and Graph chrome

Maps uses compact native-button tiles with a tool icon and exact node/connector counts; the whole tile opens the canvas with a pointer or keyboard. Decorative squares, fabricated topology and a redundant Open canvas button are omitted. Map list actions and tabs use Maps / New map / Create map; stored folder paths are not renamed. Arrange branches has a distinct branching-layout icon.

Graph starts in Link layout when no layout preference is saved; explicit saved Topic atlas choices remain respected. The breadcrumb strip and bottom-left legend are omitted. Settings and the document preview are mutually exclusive for canvas selection, search/list navigation and restored state. Opening settings hides the preview without clearing the selected highlight; selecting a document opens its preview and closes settings. Local graph keeps its explicit preview gesture. Synchronizing a highlight to the canvas does not trigger a preview.

Both canvases use the same low-contrast dot color. Graph draws a cached repeating texture into its opaque canvas, so PNG exports retain the texture and background. It follows camera movement; sparse zoom levels coarsen the spacing to avoid dense noise, and dot size stays small.
