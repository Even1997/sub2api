# Ranxi v2.21.1 and v2.21.2 integration

## Scope and acceptance

- Date: 2026-11-14 (Asia/Shanghai). Import the two release deltas after v2.21.0, not upstream/main.
- Baseline: 919f62284 (the verified local v2.21.0 main line).
- v2.21.0: b5990096ae10a0b2481182f2ac45812effacda22.
- v2.21.1: 11f250894eab310cbd4508946c44c9e2a3a2eb48.
- v2.21.2: dbed7697aa56e76c6eb48c4914fa441496b67bdd.
- Branch: codex/merge-ranxi-v2211-v2212-20261113. v2.21.1 checkpoint: 3e9a704b9.
- The original checkout remains on its original branch with all 183 tracked local modifications and untracked files preserved. They are not silently included in this release integration.
- Acceptance: compile both applications, run affected unit/database/race tests, preserve local financial and encrypted-history policies, inspect actual component screenshots, eliminate introduced failures, and document baseline failures without calling them green.
- No push, deployment, production migration, production credential access or changes to existing development services are part of this task.

## Resolution decisions

- Preserve the previous integration's deferred unified performance scheduler. Add cache weighting to the existing scoped local priority signals; the default weight is zero. Keep Teams fixed-cost recovery, paid windows, RPM/capacity/risk admission and existing latency/error evidence semantics.
- Cache statistics use the arithmetic mean of per-request percentages, scoped by model/group/reasoning effort/time window. Require one complete successful timing trace; reject invalid counters, duplicate/incomplete traces and image/video billing. Missing evidence remains distinct from zero percent.
- Keep full encrypted history portable across eligible native Responses accounts, prefer its known producer, and never rewrite the producer when another account consumes it. Response anchors and compaction remain strict. Both values of the upstream owner-binding compatibility key retain this local default.
- Preserve original input, readable history, tool results, JSON number precision and array order under a frozen digest. Skip ordinary item/reasoning normalizers for protected history. Only the opt-in omission path performs its narrowly defined normalization before freezing its sanitized snapshot.
- Preserve uncertain-delivery barriers, fresh admission and cross-node pending/commit fences. Do not replay opaque WS work or a rejected current pointer using the old warmup frame. Successful first-seen native consumption can record compatibility; loss of an observation already known by a connection fails closed.
- Integrity protection for first-seen history must not itself force the affinity scheduler. Recorded ownership and response pointers still do. This preserves original first-selection order and producer preference.
- Import opt-in HTTP history omission, provider/method fee inheritance and snapshots, API-key context token limits, per-key IP auditing/ACL masks, Anthropic 413 classification, quota-skip pending-5xx repair, video per-second labels and mobile group search behavior.
- Preserve Cloudcat product denominations, bonus exclusion, channel checks, resumable checkout, payer email, refunds and popup isolation. Provider-selection quotes use each provider's actual discounted payment base before fees and limits.
- Preserve local API-key group/package policies and independent field masks. Increment auth snapshot version to 36. Regenerate Ent instead of editing generated schema manually.
- Renumber imported migrations to 347_api_key_context_token_limit.sql, 348_key_risk_per_key_ip.sql and 349_payment_provider_fee_rates.sql; retain existing local 340-342 risk migrations. Correct the upstream VERSION file to 2.21.2.
- Adapt fixtures to local constructor/admission contracts and local migration names. Native Windows Redis fixtures use isolated loopback TCP; Linux retains isolated Unix sockets. No production Redis is used.
- Pin affected screenshot jobs to Ubuntu 24.04. Remove the omission historical RED assertion that assumes the upstream strict default. Restrict Anthropic historical RED jobs to the upstream repository; use the green quota-skip candidate gate locally. Other inherited branch-specific historical workflows are not claimed as release gates.
- Fix screenshot harness working directories so Tailwind's explicit @config resolves its real content paths. Normalize Windows glob paths. These changes affect evidence generation, not product CSS.

## Measured validation

- Frontend affected suites: 702 passing / 2 failing / 0 skipped. The exact two CreateAccountModal whitelist/mapping inspection failures also reproduce on the unchanged baseline (99 pass / 2 fail in that suite).
- Final repaired UI/cache/priority/IP-audit/video/i18n suites: 125 passing, zero failures or skips. Additional GroupsView/TablePageLayout suites: 31 passing, zero failures; final typecheck passes.
- Frontend typecheck and production bundle pass. Existing large-chunk warnings remain; no bundle-size or load-performance improvement is claimed.
- All 93 unit-tagged backend packages compile; the production server builds with its embedded frontend. Ent generation succeeded during integration.
- Real PostgreSQL/Redis repository run: 73 passing test events. Two failures reproduce against baseline production code: priority analysis cannot run inside a read-only transaction because its existing Teams anchor CTE writes, and the older 5xx claim boundary expects a completed round equal to a signal time to reject despite the existing <= SQL rule.
- The baseline database run used only an external Go overlay for the test harness, enabling the same disposable PostgreSQL/Redis endpoints. Baseline production SQL/code remained unchanged. Cancellation/deadline tests passed on this repeat; the initial timing-dependent failure is not reported as a product regression.
- Expanded backend run: 1,653 passing test events before final repairs. API-key delete cache-invalidation expectation and passthrough session.update long-context enforcement also fail on the baseline. Introduced first-selection/affinity and explicit-refusal failover regressions were isolated and repaired; final results follow below.
- Real PostgreSQL keyrisk concurrency/race: 58 passing test events, no failures/skips/race reports. Its fixture timestamps are one second behind database now() to avoid unrelated host/database clock skew.
- Additional real PostgreSQL repository race gate: 10 passing test events, zero failures/skips/race reports. Extra service-level race compilation was stopped under severe host memory pressure and is not counted as executed evidence.
- Final payment/provider/migration suite: 309 passing test events, zero failures or skips. The added discount-plus-fee selection test uses local signed popup creation, not a network checkout. Final service critical suite: 272 passing test events, zero failures or skips.
- Final handler gate: 256 passing test events, zero failures or skips, using `go test -p 2 -tags unit ./internal/handler -run 'Test(Issue715|Issue758|Issue805|OpenAI409)' -count=1 -json`. This verifies intact failover after explicit HTTP 401/403/429 rejection, single-send handling of other upstream rejections, no replay after partial/uncertain delivery, WS pointer refusal and original-history preservation. The subsequent production server build completed successfully.
- Actual Vue browser evidence passes for cache percentages/help/notice persistence and video prices in Chinese/English, light/dark. HTTP omission settings pass desktop/mobile/dark/English capture. Screenshots were inspected; these are synthetic component previews, not full-page or real-provider acceptance.
- Evidence files are outside the repository in the integration worktree parent: frontend-final.json, frontend-final-repaired.json, frontend-groups.json, backend-focused.jsonl, backend-final-critical.jsonl, handler-final-verified.jsonl, payment-final.jsonl, backend-compile.jsonl, repository-final.jsonl, repository-critical-race.jsonl, keyrisk-final.jsonl and ui-cache/ui-video/ui-encrypted. Intermediate logs retain failures before the documented repairs; the aborted service race log is not passing evidence. Baseline logs are in the baseline worktree parent, outside its archived checkout. Screenshot manifests record the checkpoint HEAD used while the candidate worktree was uncommitted; they are not claims of a pristine checkpoint render.

## Risks and completion boundary

- Existing baseline test debt remains, including the long-context session.update case. This integration is not a claim that the full baseline CI, lint or all gateway security behavior is green.
- No real paid-provider checkout, encrypted-provider interoperability, Linux release build, production traffic replay or load test was performed. Synthetic provider success cannot establish real cross-account cryptographic compatibility.
- Apply production migrations only through the normal deployment/backup process. A schema rollback is not implied by reverting the source branch.
- Completion means two reviewable local commits on an isolated branch, no unmerged paths/conflict markers, scoped validation and this ledger. It does not mean production approval.

## Quality assessment

Scores evaluate the release integration, not every pre-existing product defect. Function 9.4/10, code 9.5/10, UI 9.4/10, UX 9.4/10, performance 9.3/10, security 9.4/10; art N/A (no new artwork). Overall 9.4/10: deliverable, not a claim of high-quality completion. Below-9.5 improvements are the documented baseline failures, full-page/real-provider validation and production-representative load/Linux verification.
