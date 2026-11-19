# OAuth HTTP namespace search-history compatibility

The native OAuth provider accepts namespace tool declarations but rejects
namespace entries in `tool_choice.allowed_tools` (the actual JSON path is
`tool_choice.tools`). Automatically adding a search tool for old search history
and constructing that allowlist caused HTTP 400 on otherwise valid requests.

Native OAuth HTTP and HTTP passthrough, independent of model name, now
preserve the caller's search history, namespace declarations and tool choice
when all of these hold:

- At least one historical `web_search_call` exists and every such item is completed.
- A namespace is declared in top-level tools or a Lite `additional_tools` item.
- No search tool is already declared.
- Choice is absent/null, empty, `auto`, or `required`.
- There is no compaction trigger and the endpoint is not compact.

This only skips automatic search injection and its derived allowlist. It does
not flatten namespaces, turn historical output into text, permit new tools, or
change account availability. API-key accounts, BPS, actual WebSocket forwarding,
explicit tool choices, and incomplete histories keep their prior
policy. Optional HTTP-to-WebSocket acceleration is bypassed before dialing for
the matching request shape, regardless of model aliases or account mappings.
The routing reason is `oauth_namespace_search_history_requires_http`.
Both HTTP compatibility and acceleration routing inspect decoded JSON field
values, so equivalent Unicode escapes in keys or type names do not bypass the
policy. Requests without search history avoid full-object decoding here.
Actual WebSocket clients and explicit private WS session anchors keep their
existing transport contracts. Other accelerated requests retain existing safe
fallback gates; there is no request reconstruction or new replay after a write.

The same predicate is applied before freezing the OAuth 409 replay snapshot and
in the ordinary outgoing transformation. Original source and final wire
integrity checks remain enabled. Shared raw normalization requires an explicit
HTTP opt-in, so WebSocket callers do not silently inherit the exception.

Validation includes map/raw and Lite variants, unchanged WebSocket policy,
large JSON integers, negative scope cases, 409 final-send integrity/tampering,
and handler account-A-unavailable -> B -> namespaced tool result continuation.
Prior direct provider diagnostics on 2026-11-17 verified 14 valid HTTP/Lite
requests through one existing OAuth account's proxy with native search history
and no search declaration, across both models. This is bounded evidence, not a
provider guarantee for all accounts, models, transports or long contexts.

On 2026-11-19, eight direct provider requests on one OAuth account verified
`gpt-6.2-astra`: HTTP/Lite with auto/required selection, four correctly addressed
namespace tool calls and four successful result continuations. Synthetic test
tools did not perform external actions. The model allowlist was removed so new
model names and aliases inherit the HTTP policy. This does not claim every
future provider model has been tested. No automatic fallback to an invalid
namespace allowlist or additional post-send replay is introduced.
