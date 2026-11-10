package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCodexAppsContextRewritesDesktopSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		callID string
		output string
	}{
		{name: "open page", tool: "codex_apps_open_page", output: `{"page_id":null}`},
		{name: "selected page part", tool: "codex_apps_selected_page_part_1_of_1", output: "Companion Page snapshot"},
		{name: "later page part", tool: "codex_apps_selected_page_part_2_of_3", output: "part two"},
		{name: "empty call id", tool: "codex_apps_open_page", callID: `,"call_id":""`, output: "open"},
		{name: "whitespace call id", tool: "codex_apps_open_page", callID: `,"call_id":"  "`, output: "open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"function_call_output","namespace":"agent","name":"` + tt.tool + `","output":` + mustJSON(t, tt.output) + tt.callID + `}]}`)
			got, changed := normalizeCodexAppsContext(body)
			require.True(t, changed)
			require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
			require.Equal(t, "user", gjson.GetBytes(got, "input.0.role").String())
			require.Equal(t, tt.output, gjson.GetBytes(got, "input.0.content.0.text").String())
			require.False(t, gjson.GetBytes(got, `input.#(type=="function_call_output")`).Exists())
		})
	}
}

func TestNormalizeCodexAppsContextKeepsRealToolResults(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","previous_response_id":"resp_1","input":[{"type":"message","role":"user","content":"before"},{"type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","output":"{\"page_id\":null}"},{"type":"function_call","call_id":"call-1","name":"shell"},{"type":"function_call_output","call_id":"call-1","name":"shell","output":"done"},{"type":"message","role":"user","content":"after"}]}`)
	got, changed := normalizeCodexAppsContext(body)
	require.True(t, changed)
	require.Equal(t, "before", gjson.GetBytes(got, "input.0.content").String())
	require.Equal(t, "message", gjson.GetBytes(got, "input.1.type").String())
	require.Equal(t, `{"page_id":null}`, gjson.GetBytes(got, "input.1.content.0.text").String())
	require.Equal(t, "function_call", gjson.GetBytes(got, "input.2.type").String())
	require.Equal(t, "call-1", gjson.GetBytes(got, "input.3.call_id").String())
	require.Equal(t, "after", gjson.GetBytes(got, "input.4.content").String())
	require.Equal(t, "resp_1", gjson.GetBytes(got, "previous_response_id").String())

	again, changedAgain := normalizeCodexAppsContext(got)
	require.False(t, changedAgain)
	require.Equal(t, got, again)
}

func TestNormalizeCodexAppsContextLeavesUnsafeShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "ordinary missing call id", body: `{"input":[{"type":"function_call_output","namespace":"agent","name":"other","output":"x"}]}`},
		{name: "real call id", body: `{"input":[{"type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","call_id":"call-1","output":"x"}]}`},
		{name: "non string call id", body: `{"input":[{"type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","call_id":42,"output":"x"}]}`},
		{name: "non string output", body: `{"input":[{"type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","output":{"page_id":null}}]}`},
		{name: "other namespace", body: `{"input":[{"type":"function_call_output","namespace":"codex_app","name":"codex_apps_open_page","output":"x"}]}`},
		{name: "padded name", body: `{"input":[{"type":"function_call_output","namespace":"agent","name":" codex_apps_open_page ","output":"x"}]}`},
		{name: "non string previous response", body: `{"previous_response_id":1,"input":[{"type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","output":"x"}]}`},
		{name: "duplicate member", body: `{"input":[{"type":"function_call_output","type":"function_call_output","namespace":"agent","name":"codex_apps_open_page","output":"x"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := normalizeCodexAppsContext([]byte(tt.body))
			require.False(t, changed)
			require.Equal(t, tt.body, string(got))
		})
	}
}
