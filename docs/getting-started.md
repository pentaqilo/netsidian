# Getting started

Five minutes from setup to a working notebook.

## 1. First run

Open your instance. With account auth you'll see the **setup screen** — create your email + strong password. Loopback/desktop users land straight in the app.

## 2. Create a vault (space)

A **vault** is an independent notebook — folders, notes, graph, and settings stay inside it. Use the vault switcher to create one (e.g. "Personal"). Many small vaults beat one giant one: the graph stays readable and exports stay focused.

On a same-machine deployment with `NETSIDIAN_LOCAL_FOLDER_PICK=true`, **New vault (+)** also offers **Pick a folder…** — an existing directory (or a freshly created one) becomes the vault root in place: its Markdown files are indexed without copying, and edits write straight back to that folder. The desktop app has the equivalent native folder picker out of the box.

## 3. Notes and folders

- Create a note with one click: the **New document** buttons (document-tab **+** and the tree toolbar's *file-with-plus* icon) and `Ctrl+N` create `Untitled`, `Untitled 2`, … directly in the folder selected in the tree — no dialog; the heading text is preselected, so just type the real title and the file renames itself as you finish. Rename later from the context menu or `F2` when you know the name. Hold **Shift** (or `Ctrl+Shift+N`) when starting a note to type an exact path instead. The **folder-with-plus** icon creates folders.
- Quick note creation also works by linking: typing `[[like-this]]` to a non-existing note and clicking it creates the note.
- Drag notes into folders to move them — a compact label chip follows your cursor and the target folder gets a steady accent ring (no flicker; pausing a second on a collapsed folder opens it while you drag; dropping onto a file moves the note into that file's folder). **Images, PDFs, audio and video** in the tree open in a built-in preview (Esc or a click outside closes it; `svg` offers **Edit** in the preview header); anything else opens in a new tab. `F2` on a selected row renames it; rename and new-note/new-folder dialogs show the folder path fixed and let you edit only the name — the name is pre-selected, so just start typing to replace it. The **⋯** button that appears on hover or keyboard focus opens row actions; clicking empty tree space deselects. Delete to the **trash** — the bin icon lives next to Files / Search / Bookmarks / Tags (folder-attached vaults recycle nothing, so the bin is hidden there). Restoring and permanent purging happen in the trash view (30-day retention by default).
- Everything is a real `.md` file under the vault's content folder on disk — see [configuration](configuration.md#data-locations).

## 4. The three editors

Each tab offers:
- **Source** — CodeMirror, raw Markdown, keyboard-driven
- **Visual** — Milkdown WYSIWYG with a formatting toolbar: headings H1–H6, bold/italic/strikethrough/inline code, blockquote, fenced code blocks with language, horizontal rule, bulleted/numbered/task lists with indent/outdent and block move up/down, links (validated web URLs or vault paths), and full table support (insert, rows/columns, header row). If Netsidian can't guarantee a lossless round-trip of your document, it refuses the visual mode for that note rather than silently mangling it
- **Read** — rendered view with callouts (`> [!note]`), task lists, `==highlights==`, and dashed links for unresolved `[[targets]]`

### Diagrams, images, math

- **Mermaid** — ` ```mermaid ` fences render live in read view and in a preview tile in visual mode; diagrams follow your theme (light/dark).
- **PlantUML** — ` ```plantuml `/` ```puml ` fences (and `puml`/`plantuml` aliases) render in-browser, no external server. ` ```puml ` in notes works too.
- **Many open tabs?** Scroll the tab bar with the mouse wheel — no scrollbar strip sits over the tabs, and the active tab always stays visible.
- **Links stay inside the app** — links to notes open them, links to attachments open in a new tab, external links open in a new tab, and a link to something that does not exist simply stays inert (dotted underline, tooltip) instead of dumping you on a dead page.
- **`.puml`, `.mmd` files** — clicking one in the file tree opens a side panel with a live preview and a plain-text editor; Save keeps the exact text (conflict-checked, like notes). **`.svg`** now previews as an image with an **Edit** button that opens the same text panel.
- **Images** — paste (`Ctrl/Cmd+V`) or drop image files onto the visual editor: they upload to the vault, get embedded, and autosave with the note.
- **Math** — `$inline$` and `$$block$$` render via KaTeX; literal dollar amounts like "$5 and $10" stay text.
- **Footnotes** — `[^1]` syntax collects a Footnotes section in read view.

Edits autosave to browser drafts (~0.75 s) and commit as **revisions** — open **History** on any note to browse and restore earlier versions. Editing the same note in two windows shows a conflict prompt instead of overwriting.

## 5. Links, backlinks, graph

- `[[Note]]`, `[[Note|alias]]`, `[[folder/Note]]` and standard Markdown links all index.
- The note footer lists **backlinks**; unresolved targets get their own section.
- **Graph 2** in the ribbon shows the complete vault as a continuous canvas. Every indexed note and resolved link is included by default, including unlinked notes. Scroll to zoom, drag to pan, and click a node to open its document. Zoom never replaces notes with groups or hides connections.
- Trackpad pinch and Ctrl+wheel over the graph change only its camera. **Settings → Appearance → UI scale** offers 80%, 90%, 100%, 110%, 120%, 150% and other scales for the whole workspace.
- Find a note by title, path or `#tag` in the graph search, then select a result to center it and preview its connected notes. Search does not reduce the graph. **Graph settings** offers explicit folder/tag/file filters, unresolved targets, tag nodes, colors and force controls. Drag a node to pin its position. Fit, pause and image export are available.
- The optional **local graph** in the document inspector offers one, two or three hops. **Old graph** remains available from Graph 2 for comparison.

## 6. Search

`Ctrl/Cmd+Shift+F` → full-text search across the vault, with result counts; Enter opens, arrows navigate.

## 7. Make it yours

**Settings → Appearance**: theme and accent. Accent presets (default violet, neutral, or your custom color) pass an automatic **WCAG 4.5:1 contrast check** — pick anything readable, the app won't let you pick invisible. Below them, **Markdown colors** lets you recolor headings, links, quotes and inline code in the reader and the visual editor (each has its own Reset back to the theme default). Use the **Dark colors / Light colors** tabs to give each theme its own colors — what you edit on a tab only affects that mode, and each mode remembers its own palette. The built-in defaults already adapt to the scheme you're in: lighter hues on dark, deep readable tones on light, and **links are blue** in both — Reset on any row returns to that scheme's default.

## 8. Backup habit

**Settings → Export vault (ZIP)** downloads the full portable vault. Do it before upgrades you're nervous about; state-dir backups are covered in [deployment](deployment.md#backups).

---
Next: [deployment](deployment.md) for multi-device access, or [development](development.md) to hack on it.

## Quick capture and daily notes

Open **Memos** from the ribbon or command palette. Select a folder, type Markdown,
and click **Save Memo** beside the folder dropdown, or press **Ctrl/Cmd+Enter**.
Every memo is a regular Markdown document marked with `memo` in its frontmatter
tags. Add topic `#tags` in the body. Memos can live in any folder; ordinary notes
in that folder do not become memos.

The independent timeline folder dropdown selects **All folders**, a folder or the
vault root. Use the calendar/date picker, tags and text search to filter the whole
history, then load older results. Open/edit a memo as a regular document. Earlier
captures made before the memo marker existed can be tagged with the migration
button; existing content and revision history are preserved. The selected folder
and unfinished capture are remembered in this browser. Failed submissions can be
retried without creating a duplicate.

In the command palette (`Ctrl/Cmd+P`), choose **Open today’s daily note** to open
or create `Daily/YYYY-MM-DD.md` with Notes and Tasks sections.

## Agent access

In **Settings → Personal access tokens for agents**, select permitted vaults,
choose read-only or document write access, and set an expiration. Prepare a normal memory folder and copy the agent skill from the same settings section.
Copy the secret when shown; revoke it from the token list. Give agents the
[agent API guide](agents.md) and `/api/v1/openapi.json` contract.
