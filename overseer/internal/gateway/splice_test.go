package gateway

import (
	"encoding/json"
	"slices"
	"testing"

	"openclaw/guard/overseer/internal/capability"
)

// testRegistry mirrors testdata/tools.json.
func testRegistry() *capability.Registry {
	return capability.NewRegistry(map[string]capability.ToolCategory{
		"run_shell":   capability.CatShell,
		"sql_execute": capability.CatSQLExec,
		"sql_select":  capability.CatSQLReadOnly,
		"web_search":  capability.CatWebSearch,
		"read_file":   capability.CatFileIO,
		"weather":     capability.CatStaticAPI,
	})
}

// requestWithTools builds a request offering the named tools, each carrying a
// distinctive JSON schema so a rebuilt struct is detectable.
func requestWithTools(t *testing.T, names ...string) ChatRequest {
	t.Helper()
	tools := make([]map[string]any, 0, len(names))
	for _, n := range names {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        n,
				"description": "description of " + n,
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{n + "_arg": map[string]any{"type": "string"}},
				},
			},
		})
	}
	body, err := json.Marshal(map[string]any{
		"model": "llama3", "messages": []any{}, "tools": tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	var r ChatRequest
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func toolNames(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Function.Name)
	}
	return out
}

// --- FilterTools ------------------------------------------------------------

func TestFilterTools_KeepsOnlyWhatTheTierPermits(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "run_shell", "sql_select", "web_search", "read_file")

	full := req.FilterTools(reg, capability.ProfileFor(capability.TierFull))
	if got := toolNames(full); len(got) != 4 {
		t.Errorf("full tier kept %v, want all four", got)
	}

	// Constrained is read-only: no shell, no file I/O.
	constrained := req.FilterTools(reg, capability.ProfileFor(capability.TierConstrained))
	got := toolNames(constrained)
	if slices.Contains(got, "run_shell") || slices.Contains(got, "read_file") {
		t.Errorf("constrained tier kept a write/exec tool: %v", got)
	}
	if !slices.Contains(got, "sql_select") || !slices.Contains(got, "web_search") {
		t.Errorf("constrained tier dropped a read-only tool: %v", got)
	}
}

// Order is not noise. A reordered array is a gratuitous difference from what
// the client sent, and providers do not promise it is meaningless.
func TestFilterTools_PreservesOrder(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "web_search", "run_shell", "sql_select", "weather")

	got := toolNames(req.FilterTools(reg, capability.ProfileFor(capability.TierConstrained)))
	want := []string{"web_search", "sql_select", "weather"}
	if !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v in the original order", got, want)
	}
}

// The structs must be the originals. Function.Parameters holds the tool's whole
// JSON schema, and rebuilding would silently drop anything unmodelled.
func TestFilterTools_PreservesTheSchema(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "web_search")

	kept := req.FilterTools(reg, capability.ProfileFor(capability.TierFull))
	if len(kept) != 1 {
		t.Fatalf("kept %d tools, want 1", len(kept))
	}
	if len(kept[0].Function.Parameters) == 0 {
		t.Fatal("the tool's JSON schema was dropped")
	}
	var schema map[string]any
	if err := json.Unmarshal(kept[0].Function.Parameters, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema["properties"] == nil {
		t.Error("the schema lost its properties")
	}
	if kept[0].Function.Description == "" {
		t.Error("the tool description was dropped")
	}
	if kept[0].Type != "function" {
		t.Errorf("type = %q, want it preserved", kept[0].Type)
	}
}

// nil and []Tool{} are different instructions to a provider, and MarshalJSON
// treats them differently.
func TestFilterTools_ReturnsNilNotEmpty(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "run_shell")

	// Quarantined strips wholesale.
	if got := req.FilterTools(reg, capability.ProfileFor(capability.TierQuarantined)); got != nil {
		t.Errorf("stripped profile returned %#v, want nil so the field is omitted", got)
	}
	// Nothing permitted, but the profile does allow tools in principle.
	if got := req.FilterTools(reg, capability.ProfileFor(capability.TierConstrained)); got != nil {
		t.Errorf("no surviving tools returned %#v, want nil", got)
	}
}

// Default-deny: an unlisted name is removed even at the most permissive tier.
func TestFilterTools_DropsUnknownToolsEverywhere(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "web_search", "a_tool_nobody_registered")

	got := toolNames(req.FilterTools(reg, capability.ProfileFor(capability.TierFull)))
	if slices.Contains(got, "a_tool_nobody_registered") {
		t.Error("an unregistered tool survived the full tier")
	}
}

// --- ForcedToolName ---------------------------------------------------------

func TestForcedToolName(t *testing.T) {
	cases := []struct {
		name, choice string
		wantName     string
		wantPinned   bool
	}{
		{"absent", "", "", false},
		{"none", `"none"`, "", false},
		{"auto", `"auto"`, "", false},
		{"required", `"required"`, "", false},
		{"pinned", `{"type":"function","function":{"name":"run_shell"}}`, "run_shell", true},
		{"unrecognised object", `{"type":"something_new"}`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var r ChatRequest
			if c.choice != "" {
				r.ToolChoice = json.RawMessage(c.choice)
			}
			name, pinned := r.ForcedToolName()
			if pinned != c.wantPinned {
				t.Errorf("pinned = %v, want %v", pinned, c.wantPinned)
			}
			if name != c.wantName {
				t.Errorf("name = %q, want %q", name, c.wantName)
			}
		})
	}
}

// --- UnknownTools -----------------------------------------------------------

func TestUnknownTools(t *testing.T) {
	reg := testRegistry()

	req := requestWithTools(t, "web_search", "mystery_tool", "sql_select", "another_mystery")
	got := req.UnknownTools(reg)
	want := []string{"mystery_tool", "another_mystery"}
	if !slices.Equal(got, want) {
		t.Errorf("UnknownTools() = %v, want %v in order", got, want)
	}

	known := requestWithTools(t, "web_search", "sql_select")
	if got := known.UnknownTools(reg); got != nil {
		t.Errorf("UnknownTools() = %v, want nil when everything is registered", got)
	}

	var empty ChatRequest
	if got := empty.UnknownTools(reg); got != nil {
		t.Errorf("UnknownTools() = %v on a request with no tools", got)
	}
}

// --- Splice -----------------------------------------------------------------

func TestSplice_RemovesTheRightToolsAndReports(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "run_shell", "web_search", "sql_select")

	res := req.Splice(reg, capability.ProfileFor(capability.TierConstrained))

	if got := toolNames(req.Tools); !slices.Equal(got, []string{"web_search", "sql_select"}) {
		t.Errorf("request kept %v", got)
	}
	if !slices.Contains(res.Removed, "run_shell") {
		t.Errorf("Removed = %v, want run_shell", res.Removed)
	}
	if !slices.Equal(res.Kept, []string{"web_search", "sql_select"}) {
		t.Errorf("Kept = %v", res.Kept)
	}
}

func TestSplice_StrippedProfileOmitsTheField(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "web_search", "sql_select")

	res := req.Splice(reg, capability.ProfileFor(capability.TierQuarantined))

	if req.Tools != nil {
		t.Errorf("Tools = %#v, want nil so the field is omitted entirely", req.Tools)
	}
	if !res.AllStripped {
		t.Error("AllStripped not reported for a profile that strips wholesale")
	}

	// Confirm it really disappears from the wire.
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if _, present := m["tools"]; present {
		t.Error("tools survived serialisation; it must be absent, not []")
	}
}

// A request pinned to a tool that is no longer offered is incoherent, and
// providers reject it — the client then sees a 400 that looks like corruption
// rather than a policy decision.
func TestSplice_DropsToolChoiceWhenItsTargetIsStripped(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "run_shell", "web_search")
	req.ToolChoice = json.RawMessage(`{"type":"function","function":{"name":"run_shell"}}`)

	res := req.Splice(reg, capability.ProfileFor(capability.TierConstrained))

	if req.ToolChoice != nil {
		t.Errorf("tool_choice = %s, want it dropped: run_shell was removed", req.ToolChoice)
	}
	if !res.ToolChoiceDropped {
		t.Error("ToolChoiceDropped not reported")
	}
}

func TestSplice_LeavesToolChoiceAloneWhenItsTargetSurvives(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "run_shell", "web_search")
	original := json.RawMessage(`{"type":"function","function":{"name":"web_search"}}`)
	req.ToolChoice = original

	res := req.Splice(reg, capability.ProfileFor(capability.TierConstrained))

	if req.ToolChoice == nil {
		t.Fatal("tool_choice was dropped although web_search survived")
	}
	if string(req.ToolChoice) != string(original) {
		t.Errorf("tool_choice = %s, want it untouched", req.ToolChoice)
	}
	if res.ToolChoiceDropped {
		t.Error("ToolChoiceDropped reported when nothing was dropped")
	}
}

// "required" with no tools left compels the model to call something that does
// not exist.
func TestSplice_DropsRequiredWhenEverythingIsStripped(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "web_search")
	req.ToolChoice = json.RawMessage(`"required"`)

	res := req.Splice(reg, capability.ProfileFor(capability.TierQuarantined))

	if req.ToolChoice != nil {
		t.Errorf("tool_choice = %s, want it dropped: no tools remain to satisfy it", req.ToolChoice)
	}
	if !res.ToolChoiceDropped {
		t.Error("ToolChoiceDropped not reported")
	}
}

// "auto" and "none" stay whatever happens — neither can be invalidated.
func TestSplice_LeavesNonPinningChoicesAlone(t *testing.T) {
	reg := testRegistry()
	for _, choice := range []string{`"auto"`, `"none"`} {
		req := requestWithTools(t, "run_shell")
		req.ToolChoice = json.RawMessage(choice)

		req.Splice(reg, capability.ProfileFor(capability.TierConstrained))

		if req.ToolChoice == nil || string(req.ToolChoice) != choice {
			t.Errorf("tool_choice %s was altered to %s", choice, req.ToolChoice)
		}
	}
}

func TestSplice_ReportsUnknownToolsSeparately(t *testing.T) {
	reg := testRegistry()
	req := requestWithTools(t, "web_search", "mystery_tool")

	res := req.Splice(reg, capability.ProfileFor(capability.TierFull))

	if !slices.Equal(res.Unknown, []string{"mystery_tool"}) {
		t.Errorf("Unknown = %v, want the unregistered name", res.Unknown)
	}
	if slices.Contains(toolNames(req.Tools), "mystery_tool") {
		t.Error("an unregistered tool survived")
	}
	// A registry that has fallen behind is a different problem from a tier
	// doing its job, and the result has to say which happened.
	if slices.Contains(res.Removed, "mystery_tool") && len(res.Unknown) == 0 {
		t.Error("an unknown tool was reported only as Removed, hiding the config error")
	}
}

func TestSplice_NoToolsIsNotAnError(t *testing.T) {
	reg := testRegistry()
	var req ChatRequest
	res := req.Splice(reg, capability.ProfileFor(capability.TierConstrained))

	if req.Tools != nil || len(res.Kept) != 0 || len(res.Removed) != 0 {
		t.Errorf("a request with no tools produced %+v", res)
	}
}
