# Server40 Agent Affinity Deployment, 2026-11-12

Cutover and retirement completed at 13:04:52 Asia/Shanghai (05:04:52 UTC).
Post-retirement verification passed at approximately 13:05:52 Asia/Shanghai.

## Release Scope

- Version: `2.20.0+agent-affinity.20261112`.
- Image: `sub2api:v2.20.0-agent-affinity-20261112`.
- Image ID: `sha256:2b7ec019edd48acc314111b91580bffc3f3a93e67f3cbf282bba3550ca701ca4`.
- Live source baseline: `abd5dd740f2a3b71c536d4940fe476b63dc93fb3`.
- Production patch SHA256: `ed1eb356d3a6ce2147792fe08e73de8f360d7ac65c7209253e1ad7e42aeefc39`.
- Exactly nine production backend files changed against the live baseline.
  The patch has 90 additions and 29 deletions; tests were supplied separately.
- Existing frontend, migrations, dependencies and unrelated live fixes were retained.
  The dirty local working tree was not used as a complete release source.

Complete client history now uses best-effort parent/session account affinity.
An unavailable preferred account permits another authorized native Responses
account. Explicit native HTTP 401/403/429 rejection permits intact tool-result
failover before semantic output. Unknown delivery outcomes remain protected.
See [continuation boundaries](openai-agent-continuation.md).

## Running Targets

| Instance | Container | Local Port | Worker | Runtime Concurrency |
| --- | --- | --- | --- | --- |
| Main | `sub2api-v2.20.0-agent-affinity-20261112` | 18282 | `sub2api-openai-reauth-worker` | 3 |
| MR | `sub2api-mr-v2.20.0-agent-affinity-20261112` | 18283 | `sub2api-openai-reauth-worker-mr` | 2 |

Both workers retain their original image, environment and restart policy.
Each shares its new application's network namespace. Their configured
`http://127.0.0.1:8080` origins returned the new version, and authenticated
runtime-configuration calls succeeded. Each worker's existing recovery file
was compared by content hash against its archived predecessor and preserved.

## Verification Evidence

- Local scoped race regressions: 53 service tests plus 44 handler/repository
  tests passed, totaling 97 top-level tests. The service rerun supersedes an
  earlier test-name change during compilation.
- Existing unrelated unit-test compilation failures required an overlay
  excluding 11 test files. This is not a full repository unit-suite pass.
- Actual release build: frontend locale checks (three tests), type checking,
  frontend production build, embedded backend build and scoped service
  continuation tests passed. The latter used the staged baseline plus patch
  and updated tests, without the local unit overlay.
- Both green applications became Docker healthy before cutover.
- Environment, volumes and restart policies matched their predecessors.
- Caddy configuration validated; its three main and two MR upstream references
  point to 18282/18283, with no references to 18280/18281 remaining.
- `xymm.win`, `ai.xymm.win`, `mr.xymm.win` and `dogmaster.lol` returned healthy
  responses and the exact release version through their public settings API.
- Unauthenticated `/v1/responses` requests returned 401 on all four domains;
  these checks did not call a model provider.
- Blue inbound connections reached zero before both old applications stopped.
- Post-retirement verification passed. Both applications and workers had zero
  restarts; application startup logs had no detected panic/fatal entries.

No paid provider probe or encrypted-history extraction was performed.
Real-provider cross-account ciphertext compatibility remains unverified.
`previous_response_id` and compaction retain strict route ownership.
No load benchmark or live rollback exercise was performed.

## Rollback

Old application containers and worker containers remain available, stopped.
Worker archive names have suffix `-bak-v2.20.0-agent-affinity-20261112`.
Persistent release records are in
`/var/backups/sub2api/v2.20.0-agent-affinity-20261112` on server40.
The original Caddy configuration is also retained at
`/etc/caddy/Caddyfile.bak-v2.20.0-agent-affinity-20261112`.

From a machine with the existing SSH alias:

```powershell
ssh ssh40 "sudo -n bash /var/backups/sub2api/v2.20.0-agent-affinity-20261112/deploy.sh rollback"
```

Rollback waits for healthy blue applications, restores routing and worker
bindings, and preserves recovery records created after cutover. Green
applications with existing inbound connections remain running to drain.
It does not restore database dumps over newer production data.

Remote build, prepare, cutover, retire and final verification logs have prefix
`/tmp/v2.20.0-agent-affinity-20261112-`; local copies accompany the scoped test
evidence in the archived verification directory.
