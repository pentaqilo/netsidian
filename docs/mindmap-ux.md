# Mind map interaction decisions

The canvas combines mind mapping with editable Markdown cards. The principles below guide its first review revision.

## Connect and navigate

- Show connection points on hover and keyboard focus, with larger hit areas than their visible dots. Support dragging a point and click-to-connect.
- Highlight valid targets. A temporary connector follows the pointer; Escape or pointer cancellation leaves no permanent connection.
- Releasing on empty canvas offers a new node at that location. Canceling must create neither a file nor a dangling connector.
- Attach connectors to card borders and align Bezier tangents with border normals. Moving or resizing cards recalculates geometry.
- Pan and zoom apply to both cards and the dotted background. Account for the application's independent UI scale when translating screen coordinates.
- Keep moving a card distinct from interacting with its body, links, resize grip, and connector handles.

These decisions adapt [Miro mind map interactions](https://help.miro.com/hc/en-us/articles/360017730753-Mind-map), React Flow's [connection-on-drop pattern](https://reactflow.dev/examples/nodes/add-node-on-edge-drop), [floating edges](https://reactflow.dev/examples/edges/floating-edges), and [node resizing](https://reactflow.dev/examples/nodes/node-resizer). The application uses its existing canvas rather than introducing a second diagramming framework.

## Files and ownership

A drop is a reference operation: dropping onto a card links the source file; dropping onto empty canvas creates a Markdown card linking it. The original file stays in place. Links appear as recognizable file chips with titles, and normal Markdown links participate in existing navigation and backlinks. Explicit file moves remain separate actions.

A board metadata file owns membership, placement, size, color, and visual connector presentation. Markdown owns card content and ordinary semantic file links. Putting board layout into shared Markdown would cause one board's arrangement to change another board unexpectedly. Conversely, duplicating the same relationship independently across several files introduces partial-save and stale-data problems. Any node connection metadata must distinguish shared semantic relationships from board-specific visual connections and declare one authoritative source.

The [JSON Canvas specification](https://jsoncanvas.org/spec/1.0/) provides a useful precedent for file references, dimensions, and connector attachment sides. Netsidian retains Markdown rather than switching its existing format to JSON Canvas.

Folders can organize files or future groups. Merely adding a card to a group should not silently move its backing file. Existing maps and their collected files must remain readable; metadata evolution must preserve human-written content and foreign frontmatter.

## Compact Memos

Keep the writing area and recent thoughts prominent. Secondary filters belong on the right on wide screens and in the existing filter dialog on narrow screens. Saving remains explicit and aligned to the right. The required `memo` classification is automatic, avoiding a redundant fixed chip above every draft. Render hashtags after leaving the editor; return to the original editable text without moving the caret during typing. Default memo styling follows the main app accent while retaining explicit customization.
