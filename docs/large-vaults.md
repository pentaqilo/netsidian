# Loading large local vaults

Opening a local folder from the workspace starts a background scan and returns
the vault immediately. A status-bar progress indicator shows discovery,
indexing and link resolution. Imported notes can be read while the scan runs;
writes in that vault become available after loading. Other vaults remain usable.
An interrupted scan resumes after restart. A stopped scan exposes its error and
a Retry action. Detaching cancels pending work and preserves the original files.

The loader discovers paths without reading note bodies, then reads each eligible
note once with the configured size bound. It saves immutable revision history,
parses Markdown once, and commits metadata plus derived indexes in batches of
at most 64 notes / approximately 8 MiB of input. It resolves links once after
the documents exist, with bounded SQL write batches. New documents do not scan
and delete nonexistent search rows. Startup repair skips current indexes.

The workspace loads file metadata in pages of 500, yields between pages, and
coalesces overlapping refreshes. During import it retrieves newly indexed paths
without repeatedly scanning folder/attachment storage. Document bodies are
loaded when opened. The tree builds folder adjacency once and virtualizes large
expanded trees; selection, keyboard navigation, filtering and reveal work across
the complete logical tree.

The Playwright MCP fixture contains 5,001 Markdown notes, 1,000 attachments and
275 folders, including nested empty folders. On the acceptance machine:

- Attach response: 102 ms.
- Load plus browser verification: approximately 22.5 seconds.
- Largest measured UI heartbeat interval during import: 74 ms.
- Rendered tree rows: 40 rather than roughly 6,300.
- Eager document-body requests: zero; opening one note made one body request.

These are fixture measurements, not a guarantee for every disk or note size.
Backend tests also cover single-pass link resolution, pagination, interrupted
load recovery and cancellation on detach. Native builds compile the same
services and frontend; the browser test exercises Docker deployment.
