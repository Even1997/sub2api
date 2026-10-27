# BPS temporary credential backoff

The existing **System settings → Gateway service → BPS error recovery** switch
(`excel_bps_recovery_enabled`) controls this behavior. No new switch, duration field,
API setting, or database migration is added. The existing configurable model
rate-limit/unavailable-model cooldown is unchanged.

## Behavior

- An explicit invalid/revoked/missing grant keeps the existing whole-account
  pause policy. Credentials must be repaired and the account explicitly resumed.
- Pending Excel authorization, unavailable authorization storage, and a temporary
  token read/refresh/persistence error skip that account's **BPS routes for 30 seconds**.
  The current request uses existing bounded same-model account failover. Native
  OpenAI/Codex models, other accounts, and the account's persistent state are unchanged.
- Both schedulers, sticky/runtime eligibility, and the BPS forwarding entry check
  the backoff. A previously selected account is rejected before image preparation,
  compaction, token acquisition or an upstream business send while backoff is active.
- Expiry permits another attempt; it does not assert that credentials are healthy
  or resume a paused account. Another temporary failure schedules another 30 seconds.
- Turning the switch off stops new backoff records and makes new requests ignore
  existing records. In-flight requests retain the existing switch snapshot. Turning
  it back on honors still-unexpired records. It never resumes a persistently paused account.
- Observation/controlled tests and client cancellations do not create backoff.
  Generic background model-probe failures are not classified as credential failures.
- The cache is process-local, thread-safe, and bounded to 4096 accounts; expired
  entries are swept. Restart clears it; replicas maintain independent records. At
  capacity, existing active records are preserved and a warning is logged.

## Diagnostics and safety

`openai.excel_bps_credential_failure` records the account ID, an allowlisted
reason, and action (`cooldown_bps` or `pause_account`). Temporary failures include
`retry_at` and `cooldown_seconds`. Selection diagnostics separately count
`excel_bps_credentials_unavailable`, instead of attributing these exclusions to
model rate limits. Raw tokens, storage/provider messages, and request bodies are
not added to these logs.

This change does not increase retry budgets, choose a different model, replay
delivered content/tool calls, relax tool-history validation, or bypass user balance
checks. It addresses repeated selection of temporarily unreadable BPS credentials;
it does not guarantee recovery when every eligible account is unavailable.
