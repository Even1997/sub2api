# BPS credential recovery wait

The existing **System settings → Gateway service → BPS error recovery** switch
(`excel_bps_recovery_enabled`) controls this behavior. No new switch, duration field,
API setting, or database migration is added. The existing configurable model
rate-limit/unavailable-model cooldown is unchanged.

## Behavior

- An explicit invalid/revoked/missing grant keeps the existing whole-account
  pause policy. Credentials must be repaired and the account explicitly resumed.
- Pending Excel authorization, unavailable authorization storage, and a temporary
  token read/refresh/persistence error isolate that account's **BPS routes until a
  background credential check succeeds**.
  The current request uses existing bounded same-model account failover. Native
  OpenAI/Codex models, other accounts, and the account's persistent state are unchanged.
- Both schedulers, sticky/runtime eligibility, and the BPS forwarding entry check
  isolation. A previously selected account is rejected before image preparation,
  compaction, token acquisition or an upstream business send while isolation is active.
- After 30 seconds, the background worker checks whether authorization has finished
  and credentials can be read, validated and refreshed if needed. Pending authorization
  cannot be bypassed by an older readable grant. A completed task without usable
  credentials is insufficient. Checks send no business/model request.
- Time alone never restores routing. Failed checks retain isolation and schedule
  another check after 30 seconds; the worker scans every 15 seconds, so execution may
  be later under load. Credential and model checks share shutdown/cancellation but
  run independently. Stale or canceled check results cannot release newer failures.
- Turning the switch off stops new wait records and background checks and makes
  new requests ignore existing records. In-flight requests retain the existing
  switch snapshot. Turning it back on honors retained waits. It never resumes a
  persistently paused account or overrides the authorization worker's pending flag.
- Observation/controlled tests and client cancellations do not create waits.
  Generic background model-probe failures are not classified as credential failures.
- The cache is process-local, thread-safe, and bounded to 4096 accounts; records
  are removed after a successful check or account deletion. Restart clears it;
  replicas maintain independent records. At capacity, existing records are
  preserved and a warning is logged; unverified waits are never expired/evicted.

## Diagnostics and safety

`openai.excel_bps_credential_failure` records the account ID, an allowlisted
reason, and action (`wait_for_credentials` or `pause_account`). Temporary failures
include `next_check_at` and `check_interval_seconds`. Background checks log
`excel_bps.credential_recovery_check_failed` or `excel_bps.credential_recovery_restored`.
Selection diagnostics separately count
`excel_bps_credentials_unavailable`, instead of attributing these exclusions to
model rate limits. Raw tokens, storage/provider messages, and request bodies are
not added to these logs.

This change does not increase retry budgets, choose a different model, replay
delivered content/tool calls, relax tool-history validation, or bypass user balance
checks. It addresses repeated selection of temporarily unreadable BPS credentials;
it does not guarantee recovery when every eligible account is unavailable.
