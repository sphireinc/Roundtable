# Transactions

Every applied patch becomes a transaction.

## Transaction record

```text
TX-00017
  Proposal: P-00021
  Claims: C-00031, C-00032
  Votes: V-00051, V-00052, V-00053
  Human approval: H-00004
  Git hash before: abc123
  Git hash after: def456
  Tests: TR-00077
```

## Apply flow

1. Validate proposal status.
2. Validate active claims.
3. Validate affected resources.
4. Recompute current file/symbol hashes.
5. Reject stale base hash unless policy allows refresh.
6. Apply patch to temp copy.
7. Run static validation.
8. Apply patch to authoritative repo.
9. Record before/after git hashes.
10. Generate rollback patch.
11. Run tests.
12. Record transaction.
13. Release or update claims.
14. Emit events.

## Rollback

Roundtable should generate rollback patches where possible.

For high-risk changes, rollback notes are required before apply.
