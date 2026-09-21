package bound

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hookInput is the common subset of pre-tool hook payloads across hosts
// (Cursor preToolUse, Claude Code PreToolUse, Codex PreToolUse).
type hookInput struct {
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	Cwd       string                 `json:"cwd"`
}

// Hook reads a pre-tool-use payload on stdin and emits the host-specific
// decision on stdout. It never fails the tool call: on any internal error it
// prints nothing (hosts treat empty output as "unchanged").
func Hook(args []string, r io.Reader, w io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bound hook <cursor|claude|codex>  (reads JSON on stdin)")
		return 2
	}
	host := args[0]
	var in hookInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return 0
	}
	l := DefaultLimits()
	action, updated, reason := decide(in, l)
	if action == "allow" && updated == nil {
		if host == "cursor" {
			fmt.Fprintln(w, `{"permission":"allow"}`)
		}
		return 0
	}
	var out map[string]interface{}
	switch host {
	case "cursor":
		if action == "deny" {
			out = map[string]interface{}{"permission": "deny", "agent_message": reason, "user_message": "bound: " + firstLine(reason)}
		} else {
			out = map[string]interface{}{"permission": "allow", "updated_input": updated}
		}
	case "claude", "codex", "gemini":
		hso := map[string]interface{}{"hookEventName": "PreToolUse"}
		if action == "deny" {
			hso["permissionDecision"] = "deny"
			hso["permissionDecisionReason"] = reason
		} else {
			hso["permissionDecision"] = "allow"
			hso["updatedInput"] = updated
		}
		out = map[string]interface{}{"hookSpecificOutput": hso}
	default:
		fmt.Fprintf(os.Stderr, "bound hook: unknown host %q\n", host)
		return 2
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(out)
	return 0
}

// decide classifies the tool and returns (action, updated tool_input, reason).
func decide(in hookInput, l Limits) (string, map[string]interface{}, string) {
	name := strings.ToLower(in.ToolName)
	switch {
	case name == "shell" || name == "bash" || name == "exec_command" || name == "run_terminal_cmd":
		cmd, _ := in.ToolInput["command"].(string)
		cwd := in.Cwd
		if wd, ok := in.ToolInput["working_directory"].(string); ok && wd != "" {
			cwd = wd
		}
		d := RewriteShell(cmd, cwd, l)
		if d.Action != "rewrite" {
			return "allow", nil, ""
		}
		updated := clone(in.ToolInput)
		updated["command"] = d.Command
		return "allow", updated, d.Reason

	case name == "read" || name == "read_file" || name == "readfile":
		path := firstString(in.ToolInput, "path", "file_path", "target_file", "file")
		if path == "" {
			return "allow", nil, ""
		}
		if _, hasOff := in.ToolInput["offset"]; hasOff {
			return "allow", nil, ""
		}
		if _, hasLim := in.ToolInput["limit"]; hasLim {
			return "allow", nil, ""
		}
		if !filepath.IsAbs(path) && in.Cwd != "" {
			path = filepath.Join(in.Cwd, path)
		}
		total, size, err := countLines(path)
		if err != nil || total <= l.ReadHard {
			return "allow", nil, ""
		}
		ol := outline(path, 80)
		msg := fmt.Sprintf("bound: %s has %d lines (%s, %s). Full reads above %d lines are blocked. Read a range with offset/limit, or run `bound read %s A:B` / `bound read %s --grep RE`.\nOutline:\n%s",
			path, total, Human(size), Tokens(size), l.ReadHard, path, path, strings.Join(ol, "\n"))
		return "deny", nil, msg

	case name == "grep" || name == "grep_search" || name == "search":
		updated := clone(in.ToolInput)
		if _, ok := updated["pattern"]; !ok {
			return "allow", nil, ""
		}
		n := 0
		switch v := updated["head_limit"].(type) {
		case float64:
			n = int(v)
		case int:
			n = v
		}
		switch {
		case n == 0:
			updated["head_limit"] = l.GrepDefault
		case n > l.GrepHard:
			updated["head_limit"] = l.GrepHard
		default:
			return "allow", nil, ""
		}
		return "allow", updated, "head_limit bounded"
	}
	return "allow", nil, ""
}

func clone(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
