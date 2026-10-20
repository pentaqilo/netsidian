# Canvas navigation

Mind Map, Graph and Kanban share a floating navigation panel in the lower-right
of their working area. Navigation changes the view, never document content,
task status, file location or node layout.

- **Minus / plus:** zoom around the center of the visible working area.
- **Percentage:** restore the default 100% view.
- **Fit:** return to the content overview.
- **Map button:** show or hide the minimap. It starts collapsed to preserve space.

The minimap shows a simplified content overview and the current viewport.
Click outside the viewport rectangle to jump there. Drag the rectangle to move
without losing the original grab position; content bounds remain stable during
the gesture. Focus the overview and use arrow keys to pan, Shift+arrow for a
larger step, Home to center on content, and Escape to close and return focus to
the toggle. Toolbar arrow keys move between controls.

The panel remains outside the scaled content and beside document previews, so
controls stay usable at any canvas zoom. It is available in read-only views.
The minimap uses physical pointer-to-SVG coordinate ratios, independently of
the application's UI scale. Zoom controls stop at the view's limits.

Graph navigation uses the existing graph camera. Kanban navigation operates on
the board's visible columns and scrolling content; it does not change the order
or status of cards. The ordinary file and task workflows remain authoritative.

The design adapts the standard [React Flow controls](https://reactflow.dev/api-reference/components)
and [interactive minimap](https://reactflow.dev/api-reference/components/minimap)
patterns, plus [Miro keyboard navigation](https://help.miro.com/hc/en-us/articles/11997028019858-Keyboard-navigation-while-working-on-boards).
It uses Netsidian's existing rendering and editing engines without a new runtime
dependency.

On a short canvas (for example, above a tall mobile preview), the overview opens
as an anchored popover so it stays fully visible. Closing it restores focus to
the map button. Switching away from a hidden workspace closes that popover.
Kanban zoom is bounded to 60–150%; column widths and card density change while
text keeps a readable minimum size. Fit respects these limits, so very large
boards still scroll. Graph's default 100% is its fitted overview; mind maps use
their normal document scale.

Graph uses Link layout by default unless a saved layout overrides it. Its quiet
dotted canvas retains the dots in PNG export. Settings and document preview
share the inspector space; opening one hides the other. The former breadcrumb
strip and bottom-left legend have been removed to free canvas space.

### Moving around task boards

Drag empty board space to scroll in either direction. The Hand tool also lets you
start a pan over a task; turn it off to drag tasks between columns. Hold Space
while the board background is focused for temporary hand navigation, or use the
middle mouse button. Space on a focused task keeps the task-preview shortcut.
Escape ends hand navigation. Touch scrolling works normally outside hand mode.
Panning only changes the viewport and never changes task positions or files.

The initial minimap visibility can be changed in **Settings → Interaction**.
This applies to Maps, Graph and Boards. Retained hidden tabs wait until visible
before opening their default overview; manually closing an overview keeps it
closed as content changes.
