package tools

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	patch := func(text string) string {
		quoted, _ := json.Marshal(text)
		return string(quoted)
	}
	tests := []struct {
		name    string
		harness string
		tool    string
		args    string
		want    Action
	}{
		{"pi bash", "pi", "bash", `{"command":"ls"}`, Action{Kind: KindExec, Command: "ls"}},
		{"pi read range", "pi", "read", `{"path":"a.go","offset":5,"limit":10}`, Action{Kind: KindRead, Path: "a.go", Offset: 5, Limit: 10}},
		{"pi edit", "pi", "edit", `{"path":"a.go","edits":[{"oldText":"x","newText":"y"}]}`, Action{Kind: KindEdit, Path: "a.go", Edits: []Edit{{Old: "x", New: "y"}}}},
		{"omp bash", "omp", "bash", `{"command":"go test ./..."}`, Action{Kind: KindExec, Command: "go test ./..."}},
		{"omp bash cwd", "omp", "bash", `{"command":"go test ./...","cwd":"/w/it's"}`, Action{Kind: KindExec, Command: `cd '/w/it'\''s' && go test ./...`}},
		{"omp bash unsupported timeout", "omp", "bash", `{"command":"go test ./...","timeout":30}`, Action{Kind: KindOther}},
		{"omp read", "omp", "read", `{"path":"a.go","offset":2,"limit":4}`, Action{Kind: KindRead, Path: "a.go", Offset: 2, Limit: 4}},
		{"omp read selector", "omp", "read", `{"path":"a.go:20-40"}`, Action{Kind: KindOther}},
		{"omp read URL", "omp", "read", `{"path":"https://example.com"}`, Action{Kind: KindOther}},
		{"omp archive write", "omp", "write", `{"path":"bundle.zip:member.txt","content":"hi"}`, Action{Kind: KindOther}},
		{"omp write", "omp", "write", `{"path":"a.txt","content":"hi"}`, Action{Kind: KindWrite, Path: "a.txt", Content: "hi"}},
		{"omp pi-compatible edit", "omp", "edit", `{"path":"a.go","edits":[{"oldText":"x","newText":"y"}]}`, Action{Kind: KindEdit, Path: "a.go", Edits: []Edit{{Old: "x", New: "y"}}}},
		{"omp hashed edit", "omp", "edit", `{"path":"a.go","patch":"[a.go#1234]\\nPUT 1.=1:\\n+x"}`, Action{Kind: KindOther}},
		{"claude read", "claude", "Read", `{"file_path":"/w/a.go"}`, Action{Kind: KindRead, Path: "/w/a.go"}},
		{"claude write", "claude", "Write", `{"file_path":"a.txt","content":"hi"}`, Action{Kind: KindWrite, Path: "a.txt", Content: "hi"}},
		{"claude edit", "claude", "Edit", `{"file_path":"a.go","old_string":"x","new_string":"y"}`, Action{Kind: KindEdit, Path: "a.go", Edits: []Edit{{Old: "x", New: "y"}}}},
		{"claude multiedit", "claude", "MultiEdit", `{"file_path":"a.go","edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d"}]}`,
			Action{Kind: KindEdit, Path: "a.go", Edits: []Edit{{Old: "a", New: "b"}, {Old: "c", New: "d"}}}},
		{"claude unknown tool", "claude", "WebSearch", `{"query":"go"}`, Action{Kind: KindOther}},
		{"codex exec in workdir", "codex", "exec_command", `{"cmd":"go test ./...","workdir":"/w/it's"}`, Action{Kind: KindExec, Command: `cd '/w/it'\''s' && go test ./...`}},
		{"codex shell wrapper", "codex", "shell", `{"command":["bash","-lc","ls -la"]}`, Action{Kind: KindExec, Command: "ls -la"}},
		{"codex patch update", "codex", "apply_patch", patch("*** Begin Patch\n*** Update File: a.go\n@@ func main\n ctx\n-old\n+new\n\n@@\n-x\n+y\n*** End Patch"),
			Action{Kind: KindEdit, Path: "a.go", Edits: []Edit{{Old: "ctx\nold\n", New: "ctx\nnew\n"}, {Old: "x", New: "y"}}}},
		{"codex patch add", "codex", "apply_patch", patch("*** Begin Patch\n*** Add File: b.txt\n+one\n+two\n*** End Patch"), Action{Kind: KindWrite, Path: "b.txt", Content: "one\ntwo"}},
		{"codex patch two files", "codex", "apply_patch", patch("*** Begin Patch\n*** Update File: a\n-x\n+y\n*** Update File: b\n-x\n+y\n*** End Patch"), Action{Kind: KindOther}},
		{"codex patch delete", "codex", "apply_patch", patch("*** Begin Patch\n*** Delete File: a\n*** End Patch"), Action{Kind: KindOther}},
		{"missing required field", "pi", "read", `{}`, Action{Kind: KindOther}},
		{"malformed args", "pi", "bash", `"not an object"`, Action{Kind: KindOther}},
		{"unknown harness", "other", "bash", `{"command":"ls"}`, Action{Kind: KindOther}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Parse(test.harness, test.tool, json.RawMessage(test.args))
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Parse() = %#v, want %#v", got, test.want)
			}
		})
	}
}
