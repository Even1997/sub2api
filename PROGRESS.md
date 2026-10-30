# PR #593：独立上游渠道实体与多账号绑定

## P1 复审修复（当前进行中，覆盖此前冻结结论）

- 固定缺陷 head：`ec820522203515e67823c267badb7b01be4b8b23`。旧 head 的 push/dispatch 绿色与截图保留为历史证据，不能证明本次 P1 已闭合。
- 根因：usage LEFT JOIN 预先筛选的 cohort，正常但不属于筛选范围的账号被补 NULL，误归为 `unknown:account_missing`。
- [x] 新增私有迁移数据库的真实 HTTP→DB→GET 混合回归：显式渠道跨 URL、不同实体共享 URL、未绑定 URL fallback、无效 URL、真正 missing；请求数、金额、unknown 覆盖、分页 summary 和各维度下钻一致；读取前后 usage 不变。只有私有 fixture 暂移除 usage→account FK 以构造缺失历史记录，不改应用迁移或共享 DB。
- [x] 修复为 LEFT JOIN 完整 accounts_config 后分别筛选真正 missing 和选中 cohort，不改收入/成本公式。
- [x] 专属 CI 保留旧断言与变异回归，并新增精确旧 head 仅叠加新测试的 RED、新 head GREEN，上传日志及 run/job/event/SHA provenance。
- [x] 普通 merge origin/production @ `76b54ea02b3cd3ed8dbfb8865d1aa46c19799a5a`；PROGRESS 保留双方完整内容。账号仓储静态复核：保留 production 的 Codex snapshot 事务/顺序处理及本 PR 的初始实体绑定事务，不 rebase/force push。
- [x] 提交并普通 push 原分支，普通 merge 后 GitHub 已恢复 pull_request 事件 CI；保持 Draft，不需要 ready_for_review，仍禁止合并 PR。当前 head 的最终绿色另列下项验收。
- [ ] filter=all、全部当前 head 实际 raw 日志与 RED/GREEN provenance 复核后交 Hermes/父级独立审查。
- 首轮 `a9ca7fd36`：RED 导出路径错误（backend 工作目录下 git archive 子树），未执行测试，不能称 RED；已改从仓库根导出精确旧树并加强拒绝 build/setup/skip 的证据检查。
- 首轮安全 CI 真实失败：GHSA-83rm-6c8r-g728 与 GHSA-qr28-fpjm-wq7f。升级 vue-i18n / compiler 到11.4.16、decode-uri-component到0.5.1；本 PR 活跃前端CI使用 Node22以符合修复库引擎要求；不新增安全豁免。完整组件/语言/构建兼容性仍由新head远端CI验证。本机只更新lockfile（ignore-scripts），不运行全量前端动态。
- 本机仅静态/gofmt；禁止 Go compile/build/test/run/generate、Docker、全量前端动态。最新产物记录于 `p1-cohort/`，此前最终冻结记录已经失效。本次不改生产账本、usage、路由或凭据。

- Worktree / branch：`sub2api-profit-url-channel-remarks` / `feat/profit-url-channel-remarks`；base `production` @ `08d69a522c79a182fd681ead8fa4d848d9e1554e`。
- Draft PR：https://github.com/ranxi2001/sub2api/pull/593；同一 PR 按用户更正完成完整实体需求。
- 此前文档 head `0a91e6df1` 之后，复核补强软删保留归属、显式渠道错误记录筛选、新建实体确认和ID下钻成员展示。最终 head/checks/冻结记录统一写入产物 `entity-final-ACCEPTANCE.md` / `entity-final-freeze.json`，避免文档提交循环触发CI。
- 已读取 DEV_GUIDE 和原 PR 八节模板。PR body 保留完整模板小节；按最新实际范围更新，不创建第二 PR。
- Agent 披露：Codex / `gpt-6.1-sol` / `xhigh`；Hermes / `gpt-6.1-sol` / 推理档位未知（父级提供）。未启动子 agent。

## 完整实现

- [x] `upstream_channels` 独立实体：BIGSERIAL 稳定 ID、唯一不可变 code、唯一可编辑名称、备注、revision。
- [x] `upstream_channel_bindings` 独立账号关联：允许任意账号类型，包括不可识别/不可获取 URL；每账号 revision CAS / NULL tombstone。
- [x] 管理员实体分页 CRUD、绑定列表、分页成员、确认绑定和有界解绑；observer / 普通用户拒绝；写审计正文省略。
- [x] 账号新建（同事务）/编辑/批量选择和读回；渠道与账号管理入口可见，保存前列出成员影响。显式创建账号失败后读回可重试而不重复创建。
- [x] 删除有成员渠道返回冲突；必须先明确解绑、确认并读取新版本后删除空对象。通道 FK RESTRICT，不级联删账号/usage。软删除账号保留原绑定与历史 usage 归属；真正物理删除时由外键清绑定，migration 313触发器推进原渠道 revision。
- [x] 只有独立渠道是可写的渠道备注与身份。旧 URL-key 备注表/API/UI/迁移已撤除；不由 URL 或旧备注回填/猜测实体关系。URL 只作未绑定账号自动聚合 fallback。
- [x] 利润按显式 channel ID 聚合及下钻；同渠道可有不同 URL，不同渠道可有相同 URL。下钻筛选 channel FK 而不是 URL 字符串。未绑定 URL / unknown 明确标识；统计只是当前账号归属，不是历史路由。
- [x] 保持已确认利润口径：`usage_logs.total_cost × 当前 extra.cost_multiplier`；未设置沿用统一账户默认0.1。不叠加模型成本、账本费用倍率或改变账本/历史usage。
- [x] 中文/英文界面、独立 `/admin/upstream-channels`、账户新建/编辑/批量、精确利润页管理与直接入口，文档齐备。

## 完整验证（此前已验收 head bfb1f3804）

- 本机定向 Vitest 8 spec / 329 assertions；包含账号新建、编辑、批量、实体管理、绑定失败/读回/版本冲突、语言完整性。最新实体管理回归还实际点击取消、清空并重挂载确认，以及写后GET失败保留草稿/重新读取后确认。未运行 Go 编译/动态、Docker 或全量前端构建。
- 远端 filter=all：28 success / 3 既有条件 skipped（upstream v0.2.19 regression、CLA check/lock）；所有 check 全部完成，无失败。
- push / PR 通用 workflow 的 unit、integration、lint、安全及前端关键检查均通过；完整前端 build/typecheck 与实体专属测试均按实际 head 运行。
- 专属 PostgreSQL 变异绑定 RED 命中“explicit binding must persist independently of URL eligibility”；恢复后真实 Gin API → 私有 migration 312 DB → GET → 实际账户创建/绑定 → 利润跨页/改名/解绑/删除为 GREEN。同一实体跨不同 URL、两个实体共享相同 URL、无效 URL/动态 OAuth 都覆盖。
- 当前实体下钻 SQL 对所选 channel FK 过滤完整 cohort，防止其他账号错误落进 unknown。真实 repository 回归验证 selected total 1 / requests 4，且 JSON 历史 `usage_logs.channel_id`（模型定价）不参与实体归属。
- 浏览器：9c82752b7 的最终实体页面截图运行真实 ProfitOperationsView、UpstreamChannelsView、AccountsView 与账户新建/编辑/批量控件，使用 Axios synthetic adapter，禁止真实 `/api/`/外部访问；0 browser errors、0 live API、0 paid requests。实体浏览器产物与截图见 `/data/development/artifacts/sub2api-profit-url-channel-remarks/entity-browser-accepted-9c/`。旧 URL-key 编辑截图已废弃，不用于此次验收。
- 所有已完成及历史 raw CI 日志、filter=all 结果、修复原因与产物索引见 `/data/development/artifacts/sub2api-profit-url-channel-remarks/`。

## 最终提交与冻结门槛

- [ ] 本次收敛版文档（实体唯一身份、公式、旧 URL-key 路径已删除）提交到同一 branch。
- [ ] 最后提交 push / PR 的所有 checks `filter=all` 完成且 raw 日志复核；任何失败继续修复。
- [ ] 冻结最终 head、PR / 本地 / 远端一致且工作区干净后交父级审查。父级确认是交接点，不作为缺失功能阻塞。

完整推送 CI 后保持 Draft；不 merge / release / deploy。禁止生产 SSH/DB/Redis、付费模型；不修改模型定价 channel、路由、凭据、成本公式和历史账本。

## 最终补强与最新验收

- 软删保留显式绑定；新增私有DB回归与远端变异 RED→GREEN，物理删除另测FK清理及实体revision递增。
- 显式实体ID下钻后的成员区直接可见，留存错误记录使用同一账号cohort筛选；真实HTTP测试断言同实体4条错误，不混入其他实体/未绑定账号。
- 新建实体也需确认不可变code和零成员影响；服务端确认字段验证保持一致。组件保留失败草稿/Unicode长度回归。
- 最新完整CI未完成前不冻结；最终以产物 `entity-final-ACCEPTANCE.md`、最新filter=all原始JSON、raw日志与截图为准。源文档不写入会自引用失效的最终commit SHA。


---

# 已合入 production 的进度记录（完整保留）

# Issue #578 delivery log

## Review 2 reopened on c225 (current, acceptance blocked)

### Delivery record (source/tests frozen; parent acceptance still pending)

- Current production base incorporated normally: `12a49ef4ef6a2f116389701ac8f1fe620f27bbf5`. Preserved commit chain: merge `60254aa9081f715161ef0c0ce3b6683e671b4798` -> test-only RED `860bb76348fdab8e6d480ff17140e5d93c073e7a` -> sole production fix `2118ad29c7e52597ec1683f1de42e0835007a4a6` -> metadata fixture `e9f8f7fdaba909cdd96de79be576e304ab43d77f` -> deterministic historical RED harness `7e156717aaa09b285cff8ec00f1f8131832065e3`. This next commit changes only this log; its exact-head results must be read separately, never inferred from these source-head results.
- Read both source-head dedicated runs: push40781293857 / PR40781301008. Each actual log: service178 PASS, admin25 PASS, race x3 132 PASS; zero GREEN/race FAIL/SKIP. Go1.27.3, network only loopback. New service RED on exact merged pre-fix60254: three expected FAIL events; new real PostgreSQL/Redis RED: two top-level/four events, expected100/actual89 at generic Redis and partial DB assertions. Historical08d69 service RED7 events, 4f57 review RED16 events and storage RED2 intended100/89 failures, no Scan/build/setup/no-tests/SKIP accepted as evidence. Storage GREEN under race: four top-level/six PASS events in each push/PR, including actual scheduler selection, zero FAIL/SKIP.
- Source-head check-runs `filter=all` at log authoring: 32 total, 26 success, 5 conditional check skips, 1 PR full-integration check still running; traditional statuses0. Push full `make test-unit` / `make test-integration` completed successfully; PR unit passed and integration pending. Push/PR lint jobs succeeded (push raw shows0 issues; remaining raw archives audited when jobs complete). No pending result is recorded as a pass. Final delivery head must finish all checks and raw-log review; live final evidence is retained in PR body and `/data/development/artifacts/sub2api-issue-578/REVIEW_FIX_VALIDATION.md` after this immutable log commit.
- Actual allowed local checks: gofmt changed Go files + gofmt -l (empty), `bash -n .github/scripts/issue-578-storage.sh`, `git diff --check origin/production...HEAD`, source SHA/static assertion/production-scope audit. No local Go compile/dynamic test, Docker, full lint/build/frontend, production access or paid/model/mail call. The21 #592 production files and its entire PROGRESS record are preserved; no #592 workflow/source edits. New review2 tests are byte-identical to test-only checkpoint; no existing assertion/skip removed or weakened.
- Residuals: PostgreSQL/Redis are not one transaction; crash after DB commit/before Redis publication still needs later refresh/rebuild, and publication failure is observable on the quota CAS path. Ordinary sync retains its existing log/error contract. Observation order needs synchronized replica clocks; mixed/rolled-back binaries lack field-level CAS. Advisory publication holds DB connection/transaction across cache write; no performance claim. Transient/unclassified persistence errors retain latest fields at bounded retry rate until success/shutdown; deleted accounts terminate. Earlier DTO/presence/recovery/identity/Spark/BPS429/no-replay/auto-reset guards remain tested and unchanged.
- Agents: this recovery writer is OpenAI Codex, system model GPT-6, exact model ID/reasoning unknown (not exposed); no new subagents. Historical Codex CLI/Hermes and independent Hermes reviewer gpt-6.1-sol (reasoning unknown) declarations remain in the8-section PR body. Latest parent review's extra runtime metadata is not supplied and is not guessed. CI success cannot substitute for parent independent source review. PR591 stays Draft; no PR merge/release/deploy or production access.


- New independent source review blocks c225 despite its historical green checks: the ordinary repository single-account publisher bypasses the shared fresh-read/advisory boundary; coalescing/retry gives retained old fields a newer partial observation timestamp.
- Ordinary merge (no rebase/force) incorporated production `12a49ef4ef6a2f116389701ac8f1fe620f27bbf5` (#592). Complete conflict list: only PROGRESS.md add/add. Both #578 and #582 records are preserved; #592 source/workflow changes are unmodified. Merge/pre-fix source: `60254aa9081f715161ef0c0ce3b6683e671b4798`.
- Wrote review2 service + real PostgreSQL/Redis regressions before production edits. Dedicated workflow pins this exact merged pre-fix source for new behavioral RED, retains all historical RED/controls, and rejects build/setup errors, no-tests and SKIP as evidence. Test-only checkpoint pending remote RED; no claim of current-head acceptance.
- Current writer: OpenAI Codex (system model GPT-6; exact model ID and reasoning setting unavailable in this runtime). No new subagents spawned. Previously disclosed Hermes/independent reviewers remain recorded in PR body. Local restriction unchanged: no Go build/dynamic tests, Docker, full lint/build or production access.
- Test-only checkpoint `860bb76348fdab8e6d480ff17140e5d93c073e7a` pushed, PR confirmed open/Draft/production and mergeable (conflict resolved). Read exact push run 40778776224 / service job 123134822267 and PR run 40778780826 / service job 123134838291 new RED artifacts: both normal/retry subcases fail expected100/actual89. Push storage job 123134822012 raw/artifact proves both ordinary UpdateExtra and ClearError regress Redis100->89, and t1/t2/t3 partial pending regresses DB100->89. Exact tested pre-fix baseline is 60254aa90, no build/setup/no-tests/SKIP; no production implementation changed before these logs were read.
- GREEN implementation (remote verification pending): single repository sync now uses the shared advisory/fresh-row publication path; existing batch/rebuild/fallback/token-refresh paths audited. Pending and retry retain one value/version per field; atomic PostgreSQL UPDATE filters each field by its original version, with a frozen legacy fence for existing rows. The overall query/header timestamp cannot lend freshness to old retained fields. No unbounded queue, no per-response DB write, no retry/Close lifecycle changes.
- Implementation source `2118ad29c7e52597ec1683f1de42e0835007a4a6` pushed. Its new service review2 tests pass, but existing TestIssue578ExplicitRefreshWinsDelayedHeader fails unchanged write-count guard (expected2/actual3): the in-memory test repository misclassifies new internal field-version metadata as display and performs a spurious extra write for a rejected observation. Added metadata to the test adapter's exclusion (string literal so the pinned legacy RED source still compiles); no production/expected assertion or skip changed. Exact next-head CI mandatory; first-head failure retained in evidence.
- e9f8 push storage job 123141666325 failed before GREEN: historical 4f57 RED diagnostic attempted NULL version -> string and the old publication test released before the newer publisher necessarily finished, so it could pass by timing. This run is not valid RED/GREEN acceptance. Made only the diagnostic version nullable via COALESCE and added a bounded publisher completion handshake (fixed shared-lock code waits; rejected code publishes100 before releasing89). All original DB/Redis/selection assertions preserved; workflow now rejects SQL Scan errors as RED. New review2 tests against exact60254 baseline remain unchanged.
- Earlier source2118 PR storage job123138940503 actually passed all four top-level storage cases (six PASS events), including both new P1s and actual selection; this is source-head evidence only, not final-current-head acceptance. Final full checks/raw review and parent review remain pending.
- Adapted the existing Grok scheduler SQL mock to require begin/advisory lock/fresh read/commit, retaining every existing assertion. Local gofmt and git diff --check passed. Exact-head GREEN/full checks and independent parent review remain required; c225 green is historical only.

## Reopened after independent review (current work)

- Read the complete `parent-independent-review.md`. Prior green CI and FINAL_VALIDATION.md do **not** constitute acceptance: parent review blocks PR #591.
- P1 pending: replace process/repository-object-only ordering with an atomic shared persistence CAS and ordered cache publication; add two independent coordinator domains using the same real PostgreSQL/Redis and scheduler selection regression.
- P1 pending: audit every Codex window writer, remove DTO/time restamping bypasses in CachePostResetSnapshot and auto-reset persistFreshUsage, retain original observation/presence, and exercise actual query -> downstream seams under races and missing fields.
- P2 pending: stop/clean workers on ErrAccountNotFound, cancel waits and in-flight writes on service shutdown, preserve transient recovery, safely retire ordering state, and expose bounded non-sensitive errors; add lifecycle regressions.
- Keep same branch/Draft PR; no neighbor #592 repository Redis files/workflows or neighbor worktree edits. No local Go compilation/dynamic tests, Docker/full frontend checks, production access or merge/release/deploy.
- Preserve the parent's eight-section CONTRIBUTING PR body. Disclose Hermes independent review subAgent (`gpt-6.1-sol`, reasoning unknown). New-head remote checks/raw evidence and independent re-review remain required.
- New behavior tests written first: real QueryUsage/details -> auto-reset/post-reset stale DTO seam; permanent deletion and shutdown cancellation. Implementation now routes Codex windows through a PostgreSQL observation-version CASE/UPDATE, serializes cache publishers using transaction advisory locks plus fresh committed reads, and wraps scheduler rebuild/token-refresh publication without touching repository Redis files.
- Added dedicated shared-storage CI job: native isolated PostgreSQL/Redis inside loopback-only network namespace, two independent gateway/repository domains, actual 30-second delayed-header race, cache publication race and actual scheduler selection. The rejected head is tested RED with the same storage regressions. Dynamic results pending; this is initial review-fix increment, not acceptance.
- First review-fix increment `7a39e8594` pushed. Added follow-up coverage of the complete evaluateAccount / RunOpenAIQuotaResetPostProcess flows (query rejected, details delayed, missing usage), plus full stale account edits under the existing FOR UPDATE lock and stale scheduler publication. Native storage/RED/GREEN CI running; no local dynamic execution.
- Read first shared-storage raw logs: `7a39e8594` proves both real-DB/Redis races RED on rejected head and GREEN on new code, but artifact upload failed because PostgreSQL log was root-readable only. `d4447e786` additionally caught an uninitialized DB-fallback test fixture, so its RED is not accepted as the header/selection proof. Enabled fallback explicitly, kept every assertion, made artifact logs readable on exit and reject that fixture failure as RED. Transaction-client compatibility also corrected statically; all require new exact-head rerun.
- `43d67be06` pushed the transaction/fixture/artifact corrections. Read raw lint for d4447e786: one QF1008 redundant embedded selector in cache adapter; corrected. Service GREEN failures on early review heads are not accepted; raw logs will be read when uploaded. Added early artifact upload so service failures are available before slow RED/race compilation finishes.
- Source fixes after first run: embedded quota DTO custom decoding removed in favor of leaf-window JSON presence/observation; existing refresh status fields remain intact. CI confirmed old full backend unit test `TestGrokCatalogCASRefreshesSingleAccountOutsideTransaction` failed because single-account scheduler sync bypassed the publisher lock; restored its legacy direct path while keeping CAS and all batch/rebuild/fallback/token-refresh publishers fenced.
- Current source/delivery candidate before this log-only update: `0b294fc8c3ebc4c5c2a8803fc3d6cac08406fd53`. Its exact-head suite completed **27 success / 4 workflow skips / 0 failures** across 31 check-runs. Dedicated push+PR: baseline old-source RED (four target regressions, seven events; review blockers RED also 16 events), GREEN service 175 pass events, admin 25, race 123; both PostgreSQL/Redis jobs GREEN (old head RED two targeted storage failures; current exact head two passes). Exact Go 1.27.3, loopback-only isolation. Full backend unit/integration and push/PR lint passed; both lint logs show `0 issues`. Evidence lives in `final-exact-0b-push/`, `final-exact-0b-pr/`, `full-test-54-*.log`, and `lint-final-*-0b.log`; source hashes in `review-source-freeze.sha256`.
- The preceding exact head's two normal backend test jobs took about 10 minutes; after this documentation-only commit, all checks bind to the new SHA and are pending again. No code/test/workflow edits after source freeze; any required docs-head revalidation is not implied by preceding-head evidence.
- Final exact source head `0b294fc8c3ebc4c5c2a8803fc3d6cac08406fd53`: push run 40765395280, PR run 40765406729, and exact-head dispatch run 40764470142 all passed the dedicated regression; both ordinary push/PR full CI jobs and checks passed. `filter=all`: 31/31 completed, 27 success, 4 existing conditional skips, zero failure. Raw workflow logs/artifacts under `/data/development/artifacts/sub2api-issue-578/final-afc-push`, `final-afc-dispatch`, and `full-ci-*-afc.log`.
- Dedicated exact head: pinned production RED four intended top-level tests (7 events including subtests), review baseline RED 16 events; GREEN service 175, admin 25, race x3 123 PASS events, zero GREEN/race skip/failure. Storage old head RED 2/2, current source GREEN 2/2 in each push and PR, with actual PostgreSQL/Redis, two repo/service domains, configured threshold selection and publication race. Loopback-only runner, Go 1.27.3.
- Full push and PR `make test-unit` and `make test-integration` passed on source head; push and PR golangci-lint each report `0 issues`. No production access or paid/model/mail calls. Local Go/dynamic/Docker/full frontend restrictions were observed. `git diff --check`, gofmt and source SHA checks passed; worktree clean.
- One initial delivery-head storage setup attempt had a transient proxy HTTP/2 download failure before evidence setup; the bounded retry/evidence fix was pushed and both exact-head push/PR storage jobs subsequently passed. No unresolved CI failure on the final source or delivery head. Four skips remain skips.
- Independent Hermes review result and all three blockers are addressed in source, but a fresh parent independent source re-review is still pending. PR #591 remains Draft. No merge/release/deploy.
- Delivery-head CI first reported one shared-storage setup failure because `go mod download` received a transient proxy HTTP/2 INTERNAL_ERROR before evidence/ was created. Raw log is preserved. Added up to five bounded dependency-download attempts and early head/evidence creation; new exact-head CI is mandatory. A manual exact-head rerun is already running, but is not a substitute for push+PR check-runs.
- Raw 7a39 service log read after cancelling superseded race compilation: service tests passed; existing admin assertions failed because a custom UnmarshalJSON on embedded OpenAIQuotaUsage swallowed sibling refresh flags. Removed that DTO-level decoder, retaining presence/receipt tracking only on leaf window fields; no existing assertions changed. Rejected-head review RED itself produced expected downstream and lifecycle assertion failures.
- Exact-head shared-storage run 40751609630 (`43d67be06`) passed and artifacts downloaded: both real PostgreSQL/Redis races fail on rejected head; both pass under race on new code, including positive selection, rejection, stale edit and stale publication assertions. This is evidence for that source head only. Current d55f66fcc service/admin and full CI remain pending. Cache wrapper additionally forwards the existing bulk-reader capability to avoid losing MGET hydration behavior; DB/Redis test explicitly checks both configured threshold decisions.

- Exclusive worktree/branch: `fix/issue-578-bps-usage-snapshot`; production baseline `08d69a522c79a182fd681ead8fa4d848d9e1554e`.
- Read DEV_GUIDE.md; searched ancestors and all hidden/non-hidden workspace paths for AGENTS.md (none). Read full Issue #578 body and all comments (none), and #582 scope. No repository Redis files or neighbor workflows changed.
- User restrictions override DEV_GUIDE local dynamic checks. Executed only local gofmt/static/diff checks; no local Go compilation/tests, Docker, full frontend checks, production access, model/paid/mail calls, merge, release or deploy.
- Artifact root: `/data/development/artifacts/sub2api-issue-578`.
- Draft PR: https://github.com/ranxi2001/sub2api/pull/591 -> production, `Fixes #578`.

## Implementation and regression sequence

1. Wrote four behavior regressions before changing application code: throttled 89 -> 100 latest-value persistence, blocked-write ordering/retry, nonzero query synchronization, and all-model BPS TTL refresh/routing/throttle.
2. Initial commit/push `99147b5253d8f83b015b75d5e939b39e58b8ff88`; created Draft PR immediately with individually pending acceptance checklist.
3. Header snapshots now keep one coalesced pending update per repository/account, serialize writes, retain failures for bounded-rate retry, and reject older observations after a newer query/probe. No new unconditional 100% cooldown or threshold bypass.
4. BPS TTL uses quota queries independently of WSv2, with atomic automatic-probe reservations. OAuth credentials are reused; Spark resolves parent credentials and persists only its independent bengalfox windows.
5. QueryUsage synchronously persists explicitly present canonical windows; missing/invalid/mismatched windows cannot fabricate zero usage or reset times. RefreshQuota embeds `usage_cache_persisted`, including false on storage failure. Complete-zero recovery retains the original generation CAS; nonzero queries never clear a 429.
6. `978796e30` added threshold/credits, Spark, malformed/identity, explicit handler refresh, stale query/header ordering and concurrent TTL regressions. `e2931e0a0` added loopback-only network namespaces after dependency download. `127609e06` scoped fallback queues and asserted retry timing. No existing skip or assertion removed/weakened.
7. Initial push/PR lint failed only on the new unchecked sync.Map type assertion. Read both raw logs; `e6e930f182954fe9baa53bb5376e9724409d72b0` replaced this with a typed map. No application changes after this source freeze.

## Remote evidence actually executed

- Initial dedicated push 40739626916 and PR 40739672226: SUCCESS, including baseline behavioral RED, exact-head GREEN and race count=3. Initial full unit/integration jobs also succeeded; only the above lint checks failed.
- Frozen source head `e6e930f182954fe9baa53bb5376e9724409d72b0`: dedicated push 40740834882 and PR 40740845997 both SUCCESS. Artifact head.txt matches the full SHA. Network evidence shows only loopback (127.0.0.1 / ::1).
- Each frozen-source dedicated run: baseline RED has four expected top-level failures (7 failure events with subtests), no build/setup failure; GREEN service has 159 pass events, admin 25; race count=3 has 75 pass events. No failure or skip in GREEN/race logs. Controls include BPS 429 independent cooldown/no replay, recovery CAS/rearmed 429, identity matching, Spark, threshold=100 vs auto_pause=1.0, and reset-credit disabled/ignored-window behavior.
- Frozen-source push and PR lint raw logs both report `0 issues` (jobs 123001668653 and 123001708505). Other completed checks passed or retained their existing workflow skips. The two full unit/integration jobs are still running at this log commit, so their final result remains pending here.
- All push/PR check-runs fetched with `filter=all` for every pushed head. Raw workflow archives, individual failed/successful lint logs and dedicated evidence downloaded. `audit_ci.py`, `audit-summary.json`, `raw/` and `e6e930f18-evidence/` record the audit. Superseded intermediate runs were cancelled to free runners; cancellation is not counted as a pass. Final delivery-head audit will be kept in the same artifact directory and PR body.
- Local gofmt and git diff --check passed. Source hashes recorded in `all-source.sha256`; only this progress document changes in the delivery log commit. Exact delivery-head CI is required again; prior-head evidence is not represented as a final-head pass.

## Handoff gates

- Source frozen at the above SHA; final delivery commit only records this log. Continue this branch only if CI finds a real failure.
- Pending at log commit: exact delivery-head CI and final all-runs raw-log audit, then independent parent review. Final PR body/CI links carry the completed validation result.
- Independent parent review remains a separate gate. PR stays Draft; no merge/release/deploy authorized or performed.

# Issue #582 — Redis 6.0 compatibility

- Scope: isolated fix/issue-582-redis-compatibility branch, baseline production `08d69a522`; sole writer. Do not touch #578 service usage snapshot chain/workflow.
- Read DEV_GUIDE.md and the complete `gh issue view 582 --json body,comments` (no comments). No applicable AGENTS.md found in worktree/ancestor directories.
- Confirmed scheduler metadata uses SET GET (Redis >=6.2), freezing publication/outbox on 6.0. Official Redis command histories also confirm GETDEL/ZRANGE options >=6.2 and EXPIRE NX >=7.0.
- Chosen approach: preserve Redis 6.0 support with atomic GETSET, Lua session take/fixed-window count, and legacy sorted-set commands. Keep scheduler fencing, revision invalidation, last-used side keys, grace TTLs, and error handling.
- Additional audit hits: user message/concurrency cleanup, shared OAuth/SIWC and passkey take, IP risk/serverless sorted-set reads, API key fixed-window EXPIRE NX.
- Regression-first: add actual Redis integration behavior tests before implementation; CI-only baseline RED and exact-head GREEN on Redis 6.0/7.x. Existing tests/skips must not be weakened.
- Local policy: no Go compilation/tests, Docker, frontend suite, production SSH/DB/Redis or service changes. Only static checks/gofmt/offline scripts locally. Dynamic validation pending remote CI.
- Delivery pending: commit/push, Draft PR to production (Fixes #582), inspect all push/PR raw failures, freeze source for parent review. No merge/release/deploy.
- Regression test commit: `124864327` (tests written before implementation; no local dynamic execution).
- Initial implementation and dedicated workflow ready for review. Baseline RED restores only baseline production implementations while retaining new tests and actual Redis 6.0 container selection; GREEN restores/verifies the exact PR head.
- Static validation: gofmt, git diff --check, bash -n pass. PyYAML not installed locally; workflow YAML parsing/dynamic checks pending CI.
- Draft PR created: https://github.com/ranxi2001/sub2api/pull/592 targeting production, Fixes #582; validation explicitly pending. Initial reviewable head `4ae2584f8`.
- Static dependency review found go-redis v9.22 marks the Redis 6.0 commands deprecated in favor of 6.2-only alternatives. Added narrowly scoped staticcheck annotations explaining the compatibility requirement; no broad lint exclusions.
- The first implementation push omitted newly ignored documentation/RED script; immediately included both with force-add in `4ae2584f8`. Earlier CI runs will be triaged as superseded, not counted as acceptance.
- Remote evidence on implementation head `81b858461`: dedicated Redis 7.4.12 job passed; actual Redis 6.0 baseline RED and exact-head snapshot/outbox/session/queue race tests passed, remaining matrix unit stage pending when recorded. Current-head push golangci-lint/security/Caddy/frontend/helper checks passed; full backend/PR checks still running.
- Read and archived every failed job's raw log: old `4d3abe2c6` RED runner missing (fixed in `4ae2584f8`); old `4d3abe2c6`/`4ae2584f8` push/PR staticcheck SA1019 findings (fixed in `81b858461`, whose lint is green). No runtime failure found at this point.
- Final test hardening: capture the initial outbox ID before starting workers; read the isolated sequence after membership enqueue so immediate successful consumption/deletion cannot race the assertion. Install a CI Redis binary for the existing readiness adapter test instead of allowing its optional missing-binary skip. The pre-existing concurrency suite TODO skip is unchanged; all Issue582 cases must run.
- Final acceptance remains pending the next exact-head matrix and all push/PR checks. Live status snapshots, raw failure logs and final evidence are kept under `/data/development/artifacts/sub2api-issue-582` (`ci-runs.json`, `run-*-jobs.json`, `job-*.log`, final handoff). No local compilation/dynamic tests or production access used.
- Source will be frozen at the last green exact head for parent review. Draft PR remains unmerged; no release/deploy or service upgrade authorized/performed.
