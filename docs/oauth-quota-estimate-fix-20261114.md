# OAuth quota estimate repair, 2026-11-14

## Cause and scope

The versioned Codex usage writer bypassed the older UpdateExtra quota-sample preparation and persistence path. Read-only server40 diagnostics found current usage observations but no capacity samples newer than November 2, and no window-specific observed_at updates within the last hour. The summary also preferred these stale legacy observation timestamps over the committed per-field observation versions.

Restore capacity sample preparation inside the atomic versioned snapshot update. Derive sample and observed_at versions from each window's utilization observation, and enrich plan/rate metadata from the locked database row in the same UPDATE. Preserve field ordering, partial-window independence, reset/tiny-reading capacity retention, and metadata-only writes. The estimate reader now honors per-field observation times, including relative-reset anchoring; mixed-generation percentages and resets cannot calibrate capacity.

No billing, cache-read reduction, scheduling policy, user balance, frontend production component or migration was changed. Unknown accounts remain unknown, and the cards still report partial estimate coverage rather than claiming provider dollar balances.

## Verification

- Before repair: the new field-time regression failed for both 5h/7d and mixed observations on Windows. The old Linux production sources failed both repository sample-retention cases against disposable PostgreSQL 18 and Redis, including the pre-existing reset test.
- After repair: 553 backend unit/subtest pass events on Windows, zero failures/skips, covering quota arithmetic, auth cache, model cache-read adjustment and HTTP/WS/history boundaries.
- Four real PostgreSQL/Redis repository cases passed with CI=true and no skips. Coverage includes direct snapshot and UpdateExtra writes, coalesced per-window versions, plan/rate metadata, scheduler publication, reset/1% sample retention, older responses, metadata-only writes, eight concurrent writers, bounded cost aggregation, shared BPS usage and multiplier mismatch rejection.
- Four frontend summary display/filter tests passed. Repair the existing test fixture's missing drawer/badge stubs and token-guard API mock; frontend production code is unchanged.
- Deployment test Dockerfile now includes the quota-estimate regression names.

## Acceptance and limits

Acceptance requires real sample writes, correct per-window time anchoring, unchanged model-cache policies, healthy Main/MR origins, matching source identity, worker rebinding and public health checks. The rollout reuses the earlier blue-green script with new release names and ports, protected backups and rollback; execution evidence is recorded after rollout.

The monetary values remain estimates of observed local account costs versus upstream percentages. No synthetic capacity is assigned to unobserved accounts. Full CI and representative load testing are outside this scoped gate; existing CI limitations remain documented in the integration ledger.
