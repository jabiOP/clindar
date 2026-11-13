# Deviations from the plan

One line per deviation: what changed, and why.

- **Task 4.1:** `TestOrphanInstanceIsShownAndEditableAsOneOff` calls `svc.RSVP` and `service.ScopeNone`, which
  only exist after Tasks 4.4 and 4.2, so the 4.1 test file can't compile. The RSVP check moved to Task 4.4 as
  `TestRSVPOnOrphanInstanceNeedsNoScope` in `delete_test.go`; 4.1 keeps "shown, and fetched as a one-off".

## Ideas
- `TestUnauthorizedKeepsOpsQueued` (Task 6.2) already passes against the Task 6.1 push stub, so its red step proves
  nothing. A stronger version would fail only on the push (e.g. a `FailWrite` hook on the fake) and assert the op's
  `attempts` went up.
