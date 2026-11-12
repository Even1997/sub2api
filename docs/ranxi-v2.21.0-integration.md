# Ranxi v2.21.0 integration

## Scope and acceptance

- Import the upstream v2.20.5..v2.21.0 release delta, not upstream/main.
- Source: ranxi2001/sub2api tag v2.21.0, commit b5990096ae10a0b2481182f2ac45812effacda22.
- Local baseline: cccc8c289. Integration branch: codex/merge-ranxi-v2210-20261112.
- Candidate branch only. Do not deploy this integration to the 40 server or MR.
- Acceptance: preserve local scheduling/billing/replay protections, eliminate introduced test failures, compile both applications, and record database/Linux validation separately from unit checks.

## Resolution decisions

- Preserve flexible full-history encrypted-agent account affinity, parent preference, and unavailable-account fallback. Do not relax previous-response/compaction ownership or uncertain-delivery replay guards.
- Preserve metadata-only quality-plan updates without rediscovering an unchanged model catalog.
- Preserve local Teams paid-window economics, account admission, risk/capacity filters, scalar Max effort pricing, and Ultrafast billing.
- Defer upstream unified priority/performance scheduling changes because they conflict with those policies. Do not announce this deferred feature in the UI.
- Include opt-in Qwen native session caching and strict affinity without changing other platforms' defaults.
- Include separate OpenAI 30m/12h cache accounting and pricing, VIP credit email outbox (notification opt-in defaults false), random quality-question snapshots, bounded/cancelable 2FA imports, and protocol/auth/refund/memory fixes.
- Restore the complete video-independent price display chain from group metadata to the model plaza; reuse existing billing fields.
- Exclude upstream historical v0.2.21 acceptance artifacts and the historical twofa-import-parallel workflow/screenshot script, which depend on unrelated upstream commits.
- Exclude the added simple-mode Compose check: this release delta does not port the older environment override feature on which that check depends. The local fork's deployment defaults remain unchanged.
- Pin newly imported Playwright screenshot jobs to Ubuntu 24.04; the pinned browser package rejects the current ubuntu-latest image (26.04).

## Integration repairs

- Keep local service signatures and latest-turn admission guards. Adapt imported fixtures to authoritative readers and atomic refund reservations instead of bypassing production checks.
- Bind an admitted legacy non-batch selection through the existing sticky-session helper.
- Preserve GLM none/minimal effort values in usage metadata, matching the outgoing policy.
- Create automatic quality rules once per new 2FA account and retain cancellation.
- Ignore stale balance-history results/errors/loading completion after a new request, user change, close, or unmount.
- Preserve signed thinking/tool input envelopes, local model aliases and billing fields; fix locale compiler escapes and pricing transfer compatibility.
- Restore the baseline effort-filtered latency assertions after deferring the unified scheduler, and remove obsolete constructor placeholders from two gateway integration fixtures. The complete integration-tagged repository test binary compiles locally.

## Validation ledger

Results are measured on Windows unless stated otherwise. This ledger is updated after final checks; a passing compilation is not a deployment approval.

- Targeted backend service/handler/repository regression: 452 passing test events, no skips.
- Protocol/security/migration race checks: 1,948 passing test events across ten packages; 13 environment-dependent skips. This is not a live PostgreSQL migration test.
- Initial expanded backend run: 5,109 passing test events; failures were compared with the unchanged production baseline using a test-only overlay for pre-existing test-signature incompatibilities.
- Baseline reproduced existing first-response timing, long-context account-stat pricing, fallback usage protection, setup-token identity, terminal-stream timeout and pool-capacity/error-message failures.
- Frontend full baseline: 5,439 pass / 72 fail / 53 pending. Initial candidate: 5,561 pass / 48 fail / 53 pending. Four empty-suite errors also exist in the baseline. Newly introduced failures were isolated before repair.
- Repaired 2FA and balance-history suites: 15/15 passing. Model plaza pricing and group suites: 43/43 passing, including video rates and 12h cache prices.
- Final repaired frontend suites: 76/76 passing, no skips. This includes rule creation, balance-history request ordering, video/cache prices, group rendering and feature notices.
- Both frontend typechecking/production bundling and the backend production build pass. Ent regeneration succeeds with zero generated-file differences; all 277 changed Go files pass gofmt checking.
- Windows esbuild temp-file deletion also fails for an independent large-input transform. A nonexistent TEMP directory works for that small check but build tools recreate the directory. For the final successful Vite build, only that process's TEMP/TMP point to an existing task log file, so esbuild uses its built-in pipe fallback. No dependency or product configuration is modified.
- The current Qwen browser job (run 43186883323) passes. Desktop defaults/enabled and English mobile dark-mode screenshots were downloaded and inspected; defaults are off and controls/text remain within their containers.
- The three introduced expanded-backend failures (GLM effort metadata, WS TTL billing fixture and atomic refund fixture) now pass. Two further platform/pricing failures reproduce at the baseline. A missing Bedrock Sonnet 5.5 mapping was repaired and the entire domain package passes.
- Upstream-statistics race tests pass, including the newly added response-429 window/account regression.
- Final current-source backend regressions: 74 passing test events, no failures or skips.
- GitHub run 43186409953 passes the real PostgreSQL VIP ledger, admission, SMTP retry, concurrency and upgrade suite, and the Linux memory/mount-escape suite. Release repository tests initially failed to compile because an imported assertion referenced the deferred unified scheduler; restore the baseline effort-filtered latency assertions and include that case in the release database gate.
- Existing historical workflows are not a clean release gate: for example, run 43186883695 fails fetching historical commit 94937267b2acb32bbbd44a7aae13218709c466fc, before executing tests. Do not treat these failures as passing or rewrite unrelated workflow history in this integration.
- The current-source frontend critical CI reports 2,516 passes, 11 failures, 10 skips and one suite setup error. All eleven failed test names and the suite setup error are present in the baseline results. The local final repaired suites remain 76/76 passing.
- Lint is not green: run 43186883619 reports 113 findings. The thirteen reported locations within changed files retain the same source lines as the baseline; the remaining locations are outside this release diff. This comparison is not a claim that the entire baseline lint job was rerun.
- GitHub Security Scan passes on the candidate. A staged credential/conflict-marker scan finds no matches; diff whitespace checks pass.
- Final source commit a545bee5caace766254761a8465cbb7140d757ed passes GitHub run 43188675200: real PostgreSQL VIP ledger/admission/SMTP/concurrency/upgrades, six release repository cases (cache TTL persistence/pricing, OAuth 429 degradation/attribution, Qwen platform statistics and effort-isolated latency), and Linux memory/mount-escape regressions. The log has 51 passing test events, no failed or skipped tests and no race reports. Two historical RED workflow steps are intentionally branch-gated and do not run on this branch; they are not counted as executed tests.
- The same source passes Qwen actual-component/browser run 43188675377 and Security Scan run 43188675627. Main CI run 43188675586 also passes its Usage trend local bucket regressions after the integration fixture repairs. Full CI remains non-green for the documented baseline debt.
- Subsequent validation-ledger commits change documentation only; a545bee5caace766254761a8465cbb7140d757ed identifies the validated production and test source.

## Deployment risks

- Additive migrations 249/250/340 passed the disposable PostgreSQL upgrade and repository runs. Production data volume and migration duration remain unmeasured; no production deployment was attempted.
- Linux cgroup/symlink behavior passed the Linux runner checks; Windows alone cannot validate that boundary with the current symlink permission.
- Existing baseline test failures remain visible and are not silenced, skipped, or rewritten to assert the current behavior.
- No production load test was performed. Passing race, memory-boundary and SQL regressions does not establish production throughput or migration duration.
- SMTP acceptance followed by a crash before outbox acknowledgement can still resend an email, as documented by the upstream outbox contract. Notification is disabled unless configured.
