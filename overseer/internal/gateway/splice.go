package gateway

import (
	"openclaw/guard/overseer/internal/capability"
)

// SpliceResult records what the splice did, for logging and for the audit
// trail. The gateway needs to be able to say which tools went and why.
type SpliceResult struct {
	// Kept and Removed are tool names, in the order they arrived.
	Kept    []string
	Removed []string

	// Unknown are names absent from the registry entirely.
	//
	// Separate from Removed because they mean different things. A known tool
	// removed at tier 1 is the system working. An unknown tool is a registry
	// that has fallen behind the agent, and it is stripped even at tier 0 —
	// which README.md documents as "Full / Unrestricted". Without this
	// distinction a missing config entry looks exactly like a model regression.
	Unknown []string

	// ToolChoiceDropped is true when tool_choice named a tool that did not
	// survive, so the request fell back to the provider's default.
	ToolChoiceDropped bool

	// AllStripped is true when the profile removes tools wholesale, as opposed
	// to no supplied tool happening to be permitted.
	AllStripped bool
}

// FilterTools returns the subset of r.Tools permitted under p.
//
// TODO(jonah): yours.
//
// capability.Registry.FilterTools does the policy part and hands back permitted
// NAMES. The job here is mapping those back to the original Tool structs:
//
//   - Order must be preserved. Providers do not promise order is meaningless,
//     and a reordered array is a gratuitous difference from what the client
//     sent.
//   - The structs must be the originals, not rebuilt. Function.Parameters is a
//     json.RawMessage holding the tool's whole JSON schema; reconstructing it
//     would quietly drop anything this gateway does not model.
//   - Return nil, not []Tool{}, when nothing survives. ChatRequest.MarshalJSON
//     omits a nil tools field and emits [] for an empty one, and those are
//     different instructions to a provider.
//
// splice_test.go is the specification.
func (r ChatRequest) FilterTools(reg *capability.Registry, p capability.Profile) []Tool {
	return nil
}

// ForcedToolName returns the function name a tool_choice pins the model to.
//
// TODO(jonah): yours.
//
// tool_choice is a union. Four shapes matter:
//
//	"none"       -> no tool call wanted      -> "", false
//	"auto"       -> model decides            -> "", false
//	"required"   -> must call something      -> "", false
//	{"type":"function","function":{"name":"X"}}  -> pinned to X -> "X", true
//
// Only the last pins a specific tool, and only that one can be invalidated by
// stripping. Returning false for everything else keeps the caller simple.
//
// Unmarshal into a small struct rather than a map: a string value will fail to
// decode into a struct, and that failure is the signal that it is one of the
// first three forms. Do not treat an unrecognised shape as pinned — guessing
// would drop a field the client set deliberately.
func (r ChatRequest) ForcedToolName() (string, bool) {
	return "", false
}

// UnknownTools returns the inbound tool names the registry has never heard of.
//
// TODO(jonah): yours.
//
// A set difference between the names in r.Tools and what the registry can
// classify. capability.Registry.CategoryOf reports whether a name is known; its
// second return value is the one to look at.
//
// This is not a security check — the names are denied either way, because
// Permitted default-denies. It exists so the denial is visible. A tool missing
// from the registry file vanishes silently at every tier, and the operator sees
// a model that mysteriously stopped using a tool rather than a config error.
//
// Order preserved, nil when everything is known.
func (r ChatRequest) UnknownTools(reg *capability.Registry) []string {
	return nil
}

// Splice applies a profile to a request in place and reports what changed.
//
// TODO(jonah + claude, together): the policy, once the three functions above
// work.
//
// Shape:
//
//	result.Unknown = r.UnknownTools(reg)      // before anything is removed
//	kept := r.FilterTools(reg, p)
//	result.AllStripped = p.ToolsStripped()
//	... record Kept and Removed by comparing against the original names ...
//	r.Tools = kept                            // nil when nothing survives
//
//	if name, pinned := r.ForcedToolName(); pinned && !slices.Contains(kept names, name) {
//	    r.ToolChoice = nil
//	    result.ToolChoiceDropped = true
//	}
//
// The tool_choice rule is the subtle one. A request pinned to a tool that is no
// longer offered is incoherent, and providers reject it — so the client sees a
// 400 that looks like the gateway corrupted their request rather than a policy
// decision. Dropping the field falls back to the provider's default, which is
// a working request with fewer options.
//
// Also drop it when the profile strips everything and tool_choice is
// "required": the model would be compelled to call a tool when none exist.
func (r *ChatRequest) Splice(reg *capability.Registry, p capability.Profile) SpliceResult {
	return SpliceResult{}
}
