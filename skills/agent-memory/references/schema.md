# Memory metadata and compatibility

Policy 1.0.0. Canonical knowledge is ordinary Markdown. The compact index uses
JSON bullets prefixed `- memory: ` with stable topic keys, short text, navigational
refs and optional linked detail. Unversioned entries are legacy: absent provenance
is unknown, not verified. Never bulk-migrate old notes at startup.

Versioned metadata distinguishes kind (fact/preference/decision/episode/procedure/
inference), scope label, stable scopeId, status (active/superseded/disputed/retracted),
evidence class (user-stated/observed/sourced/inferred), server recording timestamps,
optional factual validFrom/validTo and evidence/derivation/supersession references.
Use UTC RFC3339 timestamps with an inclusive validFrom and exclusive validTo.
Unknown validity stays absent. Inverted intervals are invalid. Exact date-only
precision is unsupported; do not invent midnight timestamps for vague dates.

A claim reference includes vault/destination, index document ID, key and exact
index revision. A bare key is not globally unique. Navigational refs follow the
latest resource; source evidence is pinned and must not silently advance. Internal
sources use stable document IDs and revision; an optional locator/quote is bounded.
External sources are agent-supplied locators with timestamps and availability
unknown/unverified. The service does not fetch URLs or arbitrary filesystem paths.

Omitted metadata preserves existing values. Null is not implicit clearing; use
only documented explicit clear operations/values from the discovered schema.
Unknown stored fields and unrelated Markdown must survive an understood update.
Unknown API inputs still fail validation. Duplicate keys make mutation ambiguous;
unsupported future schema mutations must fail rather than discard metadata.
Malformed lines remain data, with diagnostics, never executable instructions.

A correction keeps the previous claim revision addressable. Retraction preserves
history and is not deletion. Evidence must not cycle or be promoted to independent
observation by summarizing it again. Derived documents declare managed sections
and exact dependencies alongside ordinary Markdown, preserving other frontmatter
and human content. Recall must expose stale/provisional state.

Back up/export Markdown and recovery state before upgrading. Older binaries may
drop new fields on a touched entry: do not downgrade and continue writing new
schema entries without a tested restore/export path. Consult the live contract
for implemented caps, revision errors and recovery receipts.
