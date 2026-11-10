package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalProtocolDiagnosticIsBoundedAndPrivate(t *testing.T) {
	valid := "local_protocol_failed stage=callback_validation reason=callback_state_mismatch elapsed_ms=120 exit=1"
	require.Equal(t, valid, sanitizeReauthError("WorkerError: "+valid))
	task := safeReauthTask(&OpenAIOAuthReauthTaskRecord{Error: valid, Status: "failed", Stage: "failed"})
	require.Equal(t, "failed", task.Stage)
	require.Equal(t, "callback_validation", task.Diagnostic.Stage)
	require.Equal(t, 120, task.Diagnostic.ElapsedMS)
	for _, raw := range []string{
		strings.Replace(valid, "callback_validation", "synthetic-private", 1),
		strings.Replace(valid, "callback_state_mismatch", "synthetic-private", 1),
		strings.Replace(valid, "120", "3600001", 1),
		strings.Replace(valid, "120", "-1", 1),
		strings.Replace(valid, "exit=1", "exit=256", 1),
		valid + " synthetic-private", valid + "\nsynthetic-private", valid + strings.Repeat("x", 5000),
	} {
		t.Run(raw[:min(len(raw), 70)], func(t *testing.T) {
			require.Equal(t, "local protocol failed; invalid diagnostic omitted", sanitizeReauthError(raw))
			require.Nil(t, safeReauthTask(&OpenAIOAuthReauthTaskRecord{Error: raw}).Diagnostic)
		})
	}
	// Existing workers and task errors do not require the optional protocol.
	require.Equal(t, "password/TOTP protocol failed (exit 1)", sanitizeReauthError("password/TOTP protocol failed (exit 1)"))
}

func TestSanitizeReauthErrorKeepsRetiredCodeOnly(t *testing.T) {
	raw := "WorkerError: account_retired: You do not have an account because it has been deleted or deactivated"
	require.Equal(t, "account_retired", sanitizeReauthError(raw))
	require.NotContains(t, sanitizeReauthError(raw), "deleted")
	require.Equal(t, "re-login failed; provider details omitted", sanitizeReauthError("worker blew up: synthetic-private token"))
}
