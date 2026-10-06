# Notifications API

Notifications are persisted workspace records, not a guarantee of browser delivery or a substitute for the associated approval/proposal/transaction state. The implementation is `api/internal/httpapi/notifications.go`. The current web shell's Notifications action is not a completed notification page.

## Routes and Fields

| Route Under `/api/v1/workspaces/{id}` | Behavior |
| --- | --- |
| `GET /notifications` | Filtered, newest-first list with timestamp/ID continuation. |
| `GET /notifications/counts` | Unread count and unread/unresolved actionable count. |
| `POST /notifications/{notification_id}/ack` | Mark a matching recipient's notification read. |

Records expose ID, workspace ID, recipient, severity, category, title/body, associated entity type/ID, read timestamp, actionable flag, resolution timestamp, and creation timestamp. Missing optional text/timestamps project as empty strings. Title, body, and recipient pass through the API text-redaction helper; other fields are not a comprehensive secret scrub. An actionable label does not encode permission to perform the linked action.

## List Selection

List scope is exact workspace ID. `recipient_id` is trimmed and defaults to trimmed `X-Actor-ID`; if both are empty, no recipient filter is applied. A requested recipient is not checked against the caller's identity by this list handler. Category/severity are trimmed exact filters rather than enum validation. `unread=true` requires `read_at IS NULL`; `actionable=true` requires flag 1 and `resolved_at IS NULL`. Only exact lowercase query value `true` enables either predicate.

Limits default to 50 and must be integers from 1 through 200. Invalid limits/cursors return `invalid_limit`/`invalid_cursor`. Ordering is creation timestamp descending, then ID descending; the cursor continues strictly below the last tuple. It is not bound to recipient or filters. See [Pagination](pagination.md) for consistency and continuation rules.

Broadcast recipient `*` is not automatically included when a specific recipient filter is applied: the list predicate is exact equality. This differs from acknowledgment eligibility. An empty recipient filter can reveal every workspace recipient's records to an otherwise permitted reader; do not infer recipient-level confidentiality from this API alone.

## Counts

Counts use the same recipient default, but do not apply list category/severity filters. `unread` counts rows with null read timestamp. `actionable` counts only those unread rows also marked actionable and unresolved. A read-but-unresolved actionable notification can still appear in `GET /notifications?actionable=true`, while not contributing to the actionable count. SQL failure returns `notification_counts_failed`, not a verified zero queue.

## Acknowledgment

Acknowledgment requires the shared authorized-human check and a resolvable workspace. It uses the raw `X-Actor-ID` for its SQL recipient predicate, while list/count defaults trim that header. Whitespace can therefore produce different read/ack behavior. The update matches notification ID, workspace, and either that exact recipient or broadcast `*`. No match returns `notification_not_found` without distinguishing absence from recipient mismatch.

The transaction sets `read_at=COALESCE(read_at, now)`, preserving an existing read timestamp. Repeated acknowledgment does not replace it. It does not set `resolved_at`, clear the actionable flag, approve a proposal, resolve a blocker, or perform an entity action. Broadcast read state is shared on one row, not a per-recipient acknowledgment table; one permitted acknowledgment can make it read for all views.

Begin failure returns `notification_ack_failed` with HTTP 500; update/commit failure uses that code with HTTP 409. After commit, the API attempts an audit row and `notification.acknowledged` event under the workspace ID; both errors are ignored. It then reloads the record. A reload failure can return 404 after the read timestamp was already saved. Successful acknowledgment does not prove audit/event delivery.

The handler itself does not require an idempotency header. If provided, common [idempotency middleware](error-semantics.md) rules apply, including non-caching of empty bodies. A repeated request may produce another audit/event even though the read timestamp remains unchanged. Inspect the notification and associated authoritative entity separately before considering the underlying work resolved.

## Creation and Retention Boundaries

These routes provide no generic public notification-create or notification-resolve action. The presence of rows depends on producer-specific behavior; neither settings `enabled` nor a connected WebSocket proves every relevant domain change creates a notification. Database retention can delete old notifications across all recipients/workspaces regardless of read/actionable/resolution state. See [Maintenance](maintenance.md) before using deletion as queue cleanup.
