# Sub2API v2.23.0 (Even)

This release selectively ports changes from upstream v2.22.0 and v2.23.0, in order, and integrates Even PR #18.

- Adds Gemini Carbon metadata, conversion and default pricing, and updates GPT-6.2 model metadata and effort levels.
- Fixes OpenAI/Anthropic tool schemas and search message conversion, and preserves search history across models in the same OAuth namespace.
- Fixes streaming cache usage, partial cache write billing, long-context exemptions, and stale quota probes. Existing Even cache discounts and custom pricing remain in place.
- Improves Claude/Kimi quota handling, Alipay refund idempotency, Bedrock settings, and VIP advance credit precision.

This release adds no database migrations and does not change the re-login worker source. Preserve worker recovery archives and configuration when rebinding workers to their new origins.

See the [port record](https://github.com/Even1997/sub2api/blob/v2.23.0/docs/ranxi-2.22-2.23-port.md) for features not ported, including the full upstream billing policy, profit wallet migrations, Vertex forwarding, and WebSocket recovery. Targeted regressions passed; the pre-existing embedded frontend test discrepancy for `/v3/other` remains.
