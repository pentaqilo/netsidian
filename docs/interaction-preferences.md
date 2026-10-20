# Interaction preferences

Open **Settings → Interaction**. Preferences belong to the signed-in user, apply
across vaults and are stored on the server with a browser cache. Reset restores
the defaults below without changing appearance or document content.

| Preference | Default | Alternatives |
|---|---|---|
| Default document mode | Reading | Visual editor, Markdown source, split |
| Double-click in Reading | Visual editor | Markdown source, no action |
| Map preview activation | Double click on header/frame | Single click |
| Edit map text on the card | Enabled | Disable and use the side preview |
| New memo composer | Visual | Markdown source |
| Canvas minimap | Starts closed | Starts open on Maps, Graph and Boards |

An explicit mode choice stays with an open document during the session; opening
a different document uses its own choice or the configured default. Closing and
reopening a tab uses the default again. Legacy workspace-wide cached modes no
longer override it. New documents open ready to type; new memos use the composer
preference. Editing shortcuts respect vault permissions. Double-clicks on links,
checkboxes, tag controls and buttons keep their existing behavior.

Double-clicking a map card body edits it in place when inline editing is enabled.
The preview gesture applies to its header/frame, avoiding conflict with text
editing and linked-file controls. F2 and the card edit button follow the same
inline-editing preference. Enter still opens the document tab.

Minimaps stay closed after you manually close them; new content does not reopen
them. Empty canvases open the configured overview when content first arrives.
Other existing controls remain available: Appearance handles font size, scale,
colors and tab restoration; graph settings handle layout and visualization;
calendar and preview separators control pane sizes.

Internal map membership comments are hidden in Reading and Visual editor views.
Raw Markdown source/export retains them, and visual edits preserve them on save.
Code examples and unrelated comments are not treated as application metadata.
