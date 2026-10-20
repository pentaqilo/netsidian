# Files, PDF tabs and import

PDF attachments open in workspace tabs. **Convert to Markdown** extracts the
selectable text into a separate note beside the PDF and keeps the original bytes.
Repeated conversion creates a new available name rather than overwriting a note.
Conversion does not reconstruct the original layout, tables or illustrations.
Image-only PDFs require OCR; no empty Markdown note is created. Unlock encrypted
PDFs before converting. Conversion is bounded to 25 MiB, 300 pages, one million
text characters and one minute of text extraction; the instance's note-size limit also applies.

Conversion uses [PDF.js](https://mozilla.github.io/pdf.js/), loaded only when
needed, with locally served worker, fonts, CMaps and codec assets. PDF contents
are processed in the browser. Original files remain subject to vault permissions.

File and folder **…** menus appear on hover and keyboard focus. Writable managed
vaults support moving notes, folders and attachments to recoverable app trash.
Restore refuses to overwrite an existing file: both the live replacement and the
trashed original remain available until the conflict is resolved. Attached folders
retain their existing trash restrictions. Moving an attachment preserves its file
type and refuses an occupied destination.

The **⊕** button on a focused folder opens a create menu for that folder: a new
document, folder, board, map or memo. Board and map entries open the same
creation dialogs as the Boards and Maps pages, prefilled with the selected
folder; a memo is created with the Memos naming convention and opens for
editing. Board and map folders — and their `board.md` / `mindmap.md`
configuration documents — carry a recognizable icon and a single-click
**Open board** / **Open map** button next to the **…** menu; the document
**…** menu offers the same open action.

Drop files from a file manager onto a folder to import there, or onto a file to
import into its parent folder. Blank workspace drops use the current destination.
Files are copied into the vault; the source files on your computer are retained.
Existing import collision choices apply. Paste in the tree imports files when the
browser exposes them through its clipboard event; ordinary text paste stays with
the focused editor. Browsers may omit filesystem files from clipboard data, so use
drag and drop or the file picker when a file-manager copy cannot be pasted.
