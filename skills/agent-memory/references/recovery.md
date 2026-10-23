# Interrupted Netsidian operations

Load only when a mutation failed, completion is unknown, or consolidation is partial.

A consolidation receipt names applied/blocked/planned steps and a recoveryRef.
Follow that authenticated REST reference through the generic read adapter. Add
includePlan=true and maxBytes=131072 when the original payload is no longer loaded.
This status lookup is read-only and principal/vault bound. It does not apply steps.
Never guess an operation ID or use a different vault to find recovery information.

Keep the original operationId and exact sources, revisions, target body, adoption
flag and entry changes. Dry-run may change to false; it is excluded from logical
payload identity. The frozen plan is capped at 96 KiB, with at most 16 sources,
one target and 32 existing entry changes. A target needs sources. Index changes
require expectedRevision; source keys require their index pin. Existing targets
require their document revision and explicit adoption when not already managed.

After a crash, ordinary startup recovery reconciles committed child revisions
into the checkpoint. Retry the original plan to finish still-pending work. A
matching status or matching text alone is not proof that a step belongs to this
request. The service checks its child operation, produced revision and exact bytes.
All steps can be saved while the final receipt remains unknown: verified:false
and outcome:partial are honest; do not announce completion or duplicate the writes.

If a human edited the target or index after partial application, preserve it.
Read the applied steps and current document, explain the blocked original plan,
and build a distinct merged plan only after the old outcome is understood. A new
identity must not be used merely to bypass a conflict. The old blocked journal
remains recovery/audit state; no automatic rollback, cancellation or purge is implied.
Pending storage-child operations need normal recovery reconciliation before retry;
do not restart a production service without appropriate operational authorization.

Remember/revise receipt loss uses the original idempotency key and identical
input. The service's durable request binding prevents a changed payload from
claiming an earlier child's result. If reconciliation cannot establish completion,
report uncertainty. Current permissions are checked again before every replay.
Retraction preserves history; deletion/backup erasure is a separate operation.
