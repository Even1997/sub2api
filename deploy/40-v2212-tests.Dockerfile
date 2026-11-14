# syntax=docker/dockerfile:1.7
ARG BUILDER_IMAGE
FROM ${BUILDER_IMAGE}
COPY backend/ /app/backend/
RUN --mount=type=cache,id=sub2api-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=sub2api-gobuild,target=/root/.cache/go-build \
    GOMAXPROCS=2 go test -p 2 -tags unit ./internal/service ./internal/handler ./internal/handler/admin ./internal/handler/dto ./internal/payment/... ./migrations \
    -run 'CacheRead|ClaudeStreaming|DuplicateGroup|ReduceClientVisibleSSE|TestIssue715|TestIssue758|TestIssue805|TestOpenAI409|Payment.*Fee|Fee.*Quote|CreateOrderFee|APIKeyService_CurrentAuthSnapshot|OpenAIQuotaCost|OpenAIQuotaCurrentUsage|NewOpenAIQuotaCostSample|OpenAIAccountSummary' -count=1 -timeout=8m
