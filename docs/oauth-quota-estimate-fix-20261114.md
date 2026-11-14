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

## Completed server40 rollout

- Deployed code commit: bbab3c5d6c09998306961f8f34f6d333aa6976c5, pushed to Even1997/sub2api main before deployment. Subsequent documentation-only commits are not part of the running binary.
- Full source archive SHA256: c578a70029abd84dd014d46157e04b48c2ed6fd84258e3448142ae13a635b5b7.
- Full frontend build (locale gate, typecheck and bundling), embedded backend build and Linux deployment regressions passed, including quota, model-cache, HTTP/WS/history, auth-cache, payment and migration scopes. This is not a full-CI claim.
- Image: sub2api:v2.21.2-quota-cost-20261114. Verified Docker image identity: sha256:67e92e40998707c97f241fac45c5380eb7fe4fc6c504baa05bd3c468f1c33fd0. Binary and image revision match the deployed code commit; version remains 2.21.2.
- Main: sub2api-v2.21.2-quota-cost-20261114, port 18290. MR: sub2api-mr-v2.21.2-quota-cost-20261114, port 18291. Three Main and two MR Caddy references switched from 18288/18289.
- Both external reauth workers were rebound to their corresponding new application network namespaces. Original environment, restart policy, image, concurrency 3/2 and one recovery archive each were preserved. Worker enabled states were restored and both heartbeats advanced after retirement; no active reauth tasks were observed.
- Cutover checks passed around 12:14 Asia/Shanghai. The first retirement check found two MR connections and left that app running. Both old inbound counts reached zero before stopping old apps around 12:18. Strict post-retirement verification passed around 12:19; apps/workers remained healthy with zero restarts.
- xymm.win, ai.xymm.win, mr.xymm.win and dogmaster.lol passed health, version, unauthenticated Responses 401 and exact public asset checks. Existing 384 frontend assets were preserved. GroupsView bundle SHA256 remains 38562e686507f45090386d043ef36f04870b98c0fca4663a535e4e77033b379b; model-cache rule fields and the full group-rule snapshots matched before/after.
- Live read-only summary probes used each instance's existing admin credential only in server memory, over loopback. No credential was printed, copied locally or changed. Main coverage increased from 3 to 58 accounts for 5h and 16 to 98 for 7d (279 total); MR increased from 8 to 12 and 40 to 65 (81 total). These are full-pool observations at different times, not fixed totals or a promise that unsampled accounts can be estimated. Both post-cutover probes completed below 0.4 seconds.
- By the final check, Main had 3 fresh 5h capacity samples and 7 fresh 7d samples dated November 14; MR had 8 and 9. Latest samples were approximately 12:19, replacing the pre-fix November 2 ceiling. Only valid observations replace retained samples; reset/tiny readings continue to retain capacity.
- The first final-check shell wrapper failed in a trailing PowerShell-expanded status assertion after Python completed its checks. The corrected strict post-retirement invocation exited successfully; no product or deployment failure was ignored.

Protected backup directory: /var/backups/sub2api/v2.21.2-quota-cost-20261114 (0700). Both dumps passed complete pg_restore decoding, with dump files 0600. Main dump SHA256: d8e4d3c60da1f6f6862962bf49ba0cfe881f6c1c00bfacdc5e757edce5bf9e7b. MR: 811bcd1b597d4e7b8816ee98f3e92cedc6730d6f247bc1cd16c794655ccf18d0. No database restore was performed.

Rollback: ssh ssh40 "sudo -n bash /var/backups/sub2api/v2.21.2-quota-cost-20261114/deploy.sh rollback". This restores prior application/worker routing without overwriting current databases. Retain /opt/sub2api-builds/v2.21.2-quota-cost-20261114 while rollback references its source-commit file. No live rollback rehearsal or representative load test was performed. The original dirty Windows checkout was not changed.

| Dimension | Score / 10 | Evidence or remaining improvement |
| --- | --- | --- |
| Function | 9.5 | Reproduced failures, exact arithmetic regressions, real PostgreSQL and recovering live samples |
| Code | 9.5 | Focused changes, version fences, concurrent writers, complete builds |
| UI | N/A | No production UI or visual changes |
| UX | 9.5 | Four display/filter tests and authenticated live summary probes |
| Art | N/A | No artwork changes |
| Performance | 9.4 | Two bounded batch queries and sub-0.4s live probes; representative load test remains |
| Security | 9.5 | Existing ordering/auth boundaries, protected backups, source identity and redacted verification |

Overall 9.4/10: deliverable within the repair scope. Representative load testing remains the below-9.5 improvement.
