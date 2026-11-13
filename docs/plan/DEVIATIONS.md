# Deviations from the plan

One line per deviation: what changed, and why.

- **Task 4.1:** `TestOrphanInstanceIsShownAndEditableAsOneOff` calls `svc.RSVP` and `service.ScopeNone`, which
  only exist after Tasks 4.4 and 4.2, so the 4.1 test file can't compile. The RSVP check moved to Task 4.4 as
  `TestRSVPOnOrphanInstanceNeedsNoScope` in `delete_test.go`; 4.1 keeps "shown, and fetched as a one-off".
- **Task 9.1:** `daemon_test.go` includes `TestUnitsRunTheGivenBinary`, which uses `daemon.Unit`/`SystemdUnit`/
  `LaunchdAgent` from Task 9.2, so the 9.1 test file can't compile and the Loop test can't run. The units test is
  added in Task 9.2 instead (with the `strings` import it needs).

## Ideas
