//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAISameAccountRetryScope(t *testing.T) {
	base := context.Background()
	owner := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	failure := &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, RetryableOnSameAccount: true, SameAccountRetryDeadline: time.Now().Add(time.Minute)}
	pinned := WithOpenAISameAccountRetry(base, owner, failure)
	require.True(t, OpenAISameAccountRetryAdmissible(pinned, 7))
	require.False(t, OpenAISameAccountRetryAdmissible(pinned, 8))
	require.True(t, OpenAISameAccountRetryAdmissible(WithOpenAISameAccountRetry(pinned, nil, nil), 8))
	canceled, cancel := context.WithCancel(pinned)
	cancel()
	require.False(t, OpenAISameAccountRetryAdmissible(canceled, 7))
	for _, account := range []*Account{nil, {ID: 7, Platform: PlatformGrok, Type: AccountTypeOAuth}, {ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}} {
		require.True(t, OpenAISameAccountRetryAdmissible(WithOpenAISameAccountRetry(base, account, failure), 8))
	}
	failure.StatusCode = http.StatusServiceUnavailable
	require.True(t, OpenAISameAccountRetryAdmissible(WithOpenAISameAccountRetry(base, owner, failure), 8))
	failure.StatusCode = http.StatusTooManyRequests
	failure.SameAccountRetryDeadline = time.Now().Add(-time.Second)
	require.False(t, OpenAISameAccountRetryAdmissible(WithOpenAISameAccountRetry(base, owner, failure), 7))
}
