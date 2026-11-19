package service

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The ChatGPT internal Codex endpoint rejects a request whose input replays a
// hosted web_search_call item unless the request also declares the web_search
// tool; the stream then fails with "response protection is unavailable".
// Codex's local context compaction sends the full history with tools:[], so
// every compaction after a web search fails (#7927).
//
// Declare a native-compatible web_search tool for such requests. Do not add
// external_web_access: the ChatGPT/Codex endpoint rejects that API-only field.
// Responses Lite
// rejects hosted tools at the top level, so Lite requests carry it in an
// input additional_tools item instead. Limit tool_choice to the caller's
// original declarations, or "none" when there were no executable tools.

const (
	openAIWebSearchCallItemType      = "web_search_call"
	openAIAdditionalToolsItemType    = "additional_tools"
	openAICompactionTriggerItemType  = "compaction_trigger"
	openAIAdditionalToolsDefaultRole = "developer"
)

var openAIWebSearchHistoryTool = map[string]any{
	"type": "web_search",
}

// Only opt in at an OAuth HTTP call site, including Lite. This is a request
// shape compatibility policy, independent of model names and account mappings.
// Keeping the caller's namespace and choice avoids an unsupported allowed_tools
// namespace entry without granting a new hosted capability. WS/compact retain
// their existing compatibility policy; this does not grant replay permission.
func preserveOpenAIHTTPNamespaceSearchHistory(body map[string]any) bool {
	if choice, exists := body["tool_choice"]; exists && choice != nil {
		value, ok := choice.(string)
		if !ok || (value != "" && value != "auto" && value != "required") {
			return false
		}
	}
	tools, _ := body["tools"].([]any)
	declared := append([]any(nil), tools...)
	input, _ := body["input"].([]any)
	found := false
	for _, raw := range input {
		item, _ := raw.(map[string]any)
		switch firstNonEmptyString(item["type"]) {
		case openAICompactionTriggerItemType:
			return false
		case openAIWebSearchCallItemType:
			if item["status"] != "completed" {
				return false
			}
			found = true
		case openAIAdditionalToolsItemType:
			additional, _ := item["tools"].([]any)
			declared = append(declared, additional...)
		}
	}
	if !found || openAIToolsContainWebSearch(declared) {
		return false
	}
	for _, raw := range declared {
		tool, _ := raw.(map[string]any)
		if tool["type"] == "namespace" {
			return true
		}
	}
	return false
}

func preserveOpenAIHTTPNamespaceSearchHistoryBody(body []byte) bool {
	if !openAIRequestHasWebSearchHistory(body) {
		return false
	}
	var decoded map[string]any
	return decodeOpenAIJSONUseNumber(body, &decoded) == nil && preserveOpenAIHTTPNamespaceSearchHistory(decoded)
}

// Inspect decoded field values, since JSON escapes must not change routing or
// compatibility policy. Avoid decoding the entire request when no search item
// exists (input may contain large image payloads).
func openAIRequestHasWebSearchHistory(body []byte) bool {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		found = strings.TrimSpace(item.Get("type").String()) == openAIWebSearchCallItemType
		return !found
	})
	return found
}

func isOpenAIWebSearchToolType(toolType string) bool {
	return strings.HasPrefix(strings.TrimSpace(toolType), "web_search")
}

func openAIToolsContainWebSearch(rawTools any) bool {
	tools, ok := rawTools.([]any)
	if !ok {
		return false
	}
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if ok && isOpenAIWebSearchToolType(firstNonEmptyString(tool["type"])) {
			return true
		}
	}
	return false
}

// Restrict selection to declarations present before the compatibility tool was
// injected. Keep explicit choices and existing allowlists; auto/required must
// not silently grant the newly added hosted tool execution permission.
func openAIHistoryToolChoice(choice any, tools []any) any {
	mode := "auto"
	switch value := choice.(type) {
	case nil:
	case string:
		switch strings.TrimSpace(value) {
		case "", "auto":
		case "required":
			mode = "required"
		default:
			return choice
		}
	case map[string]any:
		kind, _ := value["type"].(string)
		if isOpenAIWebSearchToolType(kind) {
			return "none"
		}
		if kind == "allowed_tools" {
			allowed, ok := value["tools"].([]any)
			if !ok {
				return choice
			}
			filtered := make([]any, 0, len(allowed))
			for _, raw := range allowed {
				ref, _ := raw.(map[string]any)
				typ, _ := ref["type"].(string)
				if !isOpenAIWebSearchToolType(typ) {
					filtered = append(filtered, raw)
				}
			}
			if len(filtered) != len(allowed) {
				if len(filtered) == 0 {
					return "none"
				}
				narrowed := make(map[string]any, len(value))
				for k, v := range value {
					narrowed[k] = v
				}
				narrowed["tools"] = filtered
				return narrowed
			}
		}
		return choice
	default:
		return choice
	}
	refs := openAIHistoryToolReferences(tools)
	if len(refs) == 0 {
		return "none"
	}
	return map[string]any{"type": "allowed_tools", "mode": mode, "tools": refs}
}

func openAIHistoryToolReferences(tools []any) []any {
	refs := make([]any, 0, len(tools))
	for _, raw := range tools {
		// Responses Lite accepts string declarations as named custom tools.
		// Include them in the selection policy without rewriting the declaration.
		if name, ok := raw.(string); ok {
			if strings.TrimSpace(name) != "" {
				refs = append(refs, map[string]any{"type": "custom", "name": name})
			}
			continue
		}
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := tool["type"].(string)
		if kind == "" {
			continue
		}
		ref := map[string]any{"type": kind}
		for _, key := range []string{"name", "namespace", "server_label"} {
			if value, ok := tool[key].(string); ok && value != "" {
				ref[key] = value
			}
		}
		if kind == "function" && ref["name"] == nil {
			if function, ok := tool["function"].(map[string]any); ok {
				ref["name"] = function["name"]
			}
		}
		if kind == "namespace" {
			nested, _ := tool["tools"].([]any)
			ref["tools"] = openAIHistoryToolReferences(nested)
		}
		refs = append(refs, ref)
	}
	return refs
}

// openAIAdditionalToolsInsertIndex keeps a trailing compaction trigger last,
// as required by the remote compaction v2 wire format.
func openAIAdditionalToolsInsertIndex(itemTypes []string) int {
	if n := len(itemTypes); n > 0 && itemTypes[n-1] == openAICompactionTriggerItemType {
		return n - 1
	}
	return len(itemTypes)
}

// ensureOpenAIOAuthWebSearchToolForHistory is the map variant used by the
// Codex OAuth transform.
func ensureOpenAIOAuthWebSearchToolForHistory(reqBody map[string]any, responsesLite bool) bool {
	if reqBody == nil {
		return false
	}
	input, ok := reqBody["input"].([]any)
	if !ok {
		return false
	}
	hasWebSearchCall := false
	var callerTools []any
	var additionalToolsItem map[string]any
	itemTypes := make([]string, len(input))
	for i, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemTypes[i] = strings.TrimSpace(firstNonEmptyString(item["type"]))
		switch itemTypes[i] {
		case openAIWebSearchCallItemType:
			hasWebSearchCall = true
		case openAIAdditionalToolsItemType:
			if openAIToolsContainWebSearch(item["tools"]) {
				return false
			}
			if tools, ok := item["tools"].([]any); ok {
				callerTools = append(callerTools, tools...)
			}
			if additionalToolsItem == nil {
				additionalToolsItem = item
			}
		}
	}
	if !hasWebSearchCall || openAIToolsContainWebSearch(reqBody["tools"]) {
		return false
	}
	tools, _ := reqBody["tools"].([]any)
	callerTools = append(callerTools, tools...)

	switch {
	case !responsesLite:
		reqBody["tools"] = append(tools, cloneOpenAIWebSearchHistoryTool())
	case additionalToolsItem != nil:
		existing, _ := additionalToolsItem["tools"].([]any)
		additionalToolsItem["tools"] = append(existing, cloneOpenAIWebSearchHistoryTool())
	default:
		at := openAIAdditionalToolsInsertIndex(itemTypes)
		additional := map[string]any{
			"type":  openAIAdditionalToolsItemType,
			"role":  openAIAdditionalToolsDefaultRole,
			"tools": []any{cloneOpenAIWebSearchHistoryTool()},
		}
		next := make([]any, 0, len(input)+1)
		next = append(next, input[:at]...)
		next = append(next, additional)
		next = append(next, input[at:]...)
		reqBody["input"] = next
	}
	reqBody["tool_choice"] = openAIHistoryToolChoice(reqBody["tool_choice"], callerTools)
	return true
}

// ensureOpenAIOAuthWebSearchToolForHistoryBody is the raw-body variant used by
// the passthrough and WebSocket paths; it avoids decoding the whole body.
func ensureOpenAIOAuthWebSearchToolForHistoryBody(body []byte, responsesLite bool) ([]byte, bool, error) {
	if len(body) == 0 || !bytes.Contains(body, []byte(openAIWebSearchCallItemType)) {
		return body, false, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, false, nil
	}
	items := input.Array()
	hasWebSearchCall := false
	var callerTools []any
	additionalToolsIndex := -1
	itemTypes := make([]string, len(items))
	for i, item := range items {
		itemTypes[i] = strings.TrimSpace(item.Get("type").String())
		switch itemTypes[i] {
		case openAIWebSearchCallItemType:
			hasWebSearchCall = true
		case openAIAdditionalToolsItemType:
			itemTools := item.Get("tools")
			if gjsonToolsContainWebSearch(itemTools) {
				return body, false, nil
			}
			if declared, ok := itemTools.Value().([]any); ok {
				callerTools = append(callerTools, declared...)
			}
			if additionalToolsIndex < 0 {
				additionalToolsIndex = i
			}
		}
	}
	if !hasWebSearchCall {
		return body, false, nil
	}
	tools := gjson.GetBytes(body, "tools")
	if gjsonToolsContainWebSearch(tools) {
		return body, false, nil
	}
	topLevelTools := tools.IsArray() && len(tools.Array()) > 0
	if declared, ok := tools.Value().([]any); ok {
		callerTools = append(callerTools, declared...)
	}

	var (
		next []byte
		err  error
	)
	switch {
	case !responsesLite && topLevelTools:
		next, err = sjson.SetBytes(body, "tools.-1", cloneOpenAIWebSearchHistoryTool())
	case !responsesLite:
		next, err = sjson.SetBytes(body, "tools", []any{cloneOpenAIWebSearchHistoryTool()})
	case additionalToolsIndex >= 0 && items[additionalToolsIndex].Get("tools").IsArray():
		next, err = sjson.SetBytes(body, fmt.Sprintf("input.%d.tools.-1", additionalToolsIndex), cloneOpenAIWebSearchHistoryTool())
	case additionalToolsIndex >= 0:
		next, err = sjson.SetBytes(body, fmt.Sprintf("input.%d.tools", additionalToolsIndex), []any{cloneOpenAIWebSearchHistoryTool()})
	default:
		next, err = insertOpenAIAdditionalToolsItemRaw(body, items, openAIAdditionalToolsInsertIndex(itemTypes))
	}
	if err != nil {
		return body, false, fmt.Errorf("declare web_search tool for web_search_call history: %w", err)
	}
	next, err = sjson.SetBytes(next, "tool_choice", openAIHistoryToolChoice(gjson.GetBytes(body, "tool_choice").Value(), callerTools))
	if err != nil {
		return body, false, fmt.Errorf("restrict tool_choice for web_search_call history: %w", err)
	}
	return next, true, nil
}

func insertOpenAIAdditionalToolsItemRaw(body []byte, items []gjson.Result, at int) ([]byte, error) {
	additional, err := marshalOpenAIUpstreamJSON(map[string]any{
		"type":  openAIAdditionalToolsItemType,
		"role":  openAIAdditionalToolsDefaultRole,
		"tools": []any{cloneOpenAIWebSearchHistoryTool()},
	})
	if err != nil {
		return nil, err
	}
	rawItems := make([][]byte, 0, len(items)+1)
	for i, item := range items {
		if i == at {
			rawItems = append(rawItems, additional)
		}
		rawItems = append(rawItems, []byte(item.Raw))
	}
	if at >= len(items) {
		rawItems = append(rawItems, additional)
	}
	raw := append([]byte{'['}, bytes.Join(rawItems, []byte{','})...)
	raw = append(raw, ']')
	return sjson.SetRawBytes(body, "input", raw)
}

func gjsonToolsContainWebSearch(tools gjson.Result) bool {
	if !tools.IsArray() {
		return false
	}
	for _, tool := range tools.Array() {
		if isOpenAIWebSearchToolType(tool.Get("type").String()) {
			return true
		}
	}
	return false
}

func cloneOpenAIWebSearchHistoryTool() map[string]any {
	tool := make(map[string]any, len(openAIWebSearchHistoryTool))
	for key, value := range openAIWebSearchHistoryTool {
		tool[key] = value
	}
	return tool
}
