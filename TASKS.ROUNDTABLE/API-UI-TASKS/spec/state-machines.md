# Core State Machines

## Proposal

`draft/internal -> proposed -> voting -> consensus_passed|consensus_rejected -> policy_check -> approval_required|ready -> staged -> transaction_in_progress -> committed|failed|cancelled`

A proposal can be sent to `changes_requested` from human review, then return through validation/voting according to governance policy.

## Claim

`requested -> acquired -> released|expired`

Conflict path:

`requested -> contested -> acquired|rejected|cancelled`

Forced release is recorded as an action, not a hidden state rewrite.

## Session

`starting -> active -> idle|waiting -> paused -> active -> stopping -> completed`

Failure path:

`starting|active|idle|waiting -> disconnected|crashed`

Recovery path:

`disconnected|crashed|completed(resumable) -> resuming -> active`

## Transaction

`queued -> validating -> blocked|staged -> applying -> verifying -> committed`

Failure paths:

`validating|applying|verifying -> failed`

Human/system cancellation is permitted only in server-defined interruptible states.

## Approval

`required -> pending -> approved|rejected|changes_requested|deferred`

## Policy revision

`draft -> validated -> published(active) -> superseded`

Published revisions are immutable.
