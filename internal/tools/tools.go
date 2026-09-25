// Package tools maps harness-specific tool calls onto a small set of
// harness-neutral actions, so an exporter can re-emit a call as the target
// harness's equivalent tool.
package tools

import (
	"encoding/json"
	"strings"
)

type Kind string

const (
	KindExec  Kind = "exec"  // run a shell command
	KindRead  Kind = "read"  // read one file
	KindWrite Kind = "write" // create or overwrite one file
	KindEdit  Kind = "edit"  // replace text in one file
	KindOther Kind = "other" // no neutral equivalent
)

// Action is a tool call in harness-neutral terms. Only the fields that
// belong to Kind are set.
type Action struct {
	Kind    Kind
	Command string // exec
	Path    string // read, write, edit
	Offset  int    // read: first line, 1-based; 0 when absent
	Limit   int    // read: line count; 0 when absent
	Content string // write
	Edits   []Edit // edit
}

// Edit replaces Old with New.
type Edit struct {
	Old string
	New string
}

var other = Action{Kind: KindOther}

// Parse maps one recorded call from harness onto an Action. Unknown tools and
// arguments that do not fit the expected shape become KindOther.
func Parse(harness, name string, args json.RawMessage) Action {
	switch harness {
	case "pi":
		return parsePi(name, args)
	case "claude":
		return parseClaude(name, args)
	case "codex":
		return parseCodex(name, args)
	}
	return other
}

func parsePi(name string, args json.RawMessage) Action {
	var a struct {
		Command string `json:"command"`
		Path    string `json:"path"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
		Content string `json:"content"`
		Edits   []struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		} `json:"edits"`
	}
	if json.Unmarshal(args, &a) != nil {
		return other
	}
	switch name {
	case "bash":
		return exec(a.Command)
	case "read":
		return read(a.Path, a.Offset, a.Limit)
	case "write":
		return write(a.Path, a.Content)
	case "edit":
		edits := make([]Edit, len(a.Edits))
		for i, e := range a.Edits {
			edits[i] = Edit{Old: e.OldText, New: e.NewText}
		}
		return edit(a.Path, edits)
	}
	return other
}

func parseClaude(name string, args json.RawMessage) Action {
	type claudeEdit struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	var a struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
		Content  string `json:"content"`
		claudeEdit
		Edits []claudeEdit `json:"edits"` // MultiEdit
	}
	if json.Unmarshal(args, &a) != nil {
		return other
	}
	switch name {
	case "Bash":
		return exec(a.Command)
	case "Read":
		return read(a.FilePath, a.Offset, a.Limit)
	case "Write":
		return write(a.FilePath, a.Content)
	case "Edit":
		return edit(a.FilePath, []Edit{{Old: a.OldString, New: a.NewString}})
	case "MultiEdit":
		edits := make([]Edit, len(a.Edits))
		for i, e := range a.Edits {
			edits[i] = Edit{Old: e.OldString, New: e.NewString}
		}
		return edit(a.FilePath, edits)
	}
	return other
}

func parseCodex(name string, args json.RawMessage) Action {
	switch name {
	case "exec_command":
		var a struct {
			Cmd     string `json:"cmd"`
			Workdir string `json:"workdir"`
		}
		if json.Unmarshal(args, &a) != nil {
			return other
		}
		if a.Workdir != "" && a.Cmd != "" {
			return exec("cd " + shellQuote(a.Workdir) + " && " + a.Cmd)
		}
		return exec(a.Cmd)
	case "shell":
		var a struct {
			Command []string `json:"command"`
			Workdir string   `json:"workdir"`
		}
		if json.Unmarshal(args, &a) != nil {
			return other
		}
		command := strings.Join(a.Command, " ")
		// Codex wraps commands as ["bash", "-lc", "<script>"].
		if len(a.Command) == 3 && a.Command[1] == "-lc" {
			command = a.Command[2]
		}
		if a.Workdir != "" && command != "" {
			command = "cd " + shellQuote(a.Workdir) + " && " + command
		}
		return exec(command)
	case "apply_patch":
		return parsePatch(patchText(args))
	}
	return other
}

// patchText extracts apply_patch input: custom tool calls store the patch as
// a JSON string, function calls as {"input": "..."}.
func patchText(args json.RawMessage) string {
	var text string
	if json.Unmarshal(args, &text) == nil {
		return text
	}
	var a struct {
		Input string `json:"input"`
	}
	_ = json.Unmarshal(args, &a)
	return a.Input
}

// parsePatch maps a Codex patch that touches exactly one file onto an edit
// (Update File) or a write (Add File). Deletes, moves, and multi-file patches
// have no single-file equivalent and become KindOther.
func parsePatch(patch string) Action {
	var (
		section, op, path  string // section: the file block being read; "" outside one
		files              int
		added              []string
		edits              []Edit
		oldLines, newLines []string
	)
	flushHunk := func() {
		if len(oldLines) > 0 || len(newLines) > 0 {
			edits = append(edits, Edit{Old: strings.Join(oldLines, "\n"), New: strings.Join(newLines, "\n")})
		}
		oldLines, newLines = nil, nil
	}
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case line == "*** Begin Patch", line == "*** End of File":
		case line == "*** End Patch":
			section = ""
		case strings.HasPrefix(line, "*** Update File: "):
			files++
			section, op, path = "update", "update", strings.TrimPrefix(line, "*** Update File: ")
		case strings.HasPrefix(line, "*** Add File: "):
			files++
			section, op, path = "add", "add", strings.TrimPrefix(line, "*** Add File: ")
		case strings.HasPrefix(line, "*** "): // Delete File, Move to
			return other
		case section == "add":
			added = append(added, strings.TrimPrefix(line, "+"))
		case section == "update" && strings.HasPrefix(line, "@@"):
			flushHunk()
		case section == "update" && line == "":
			// Some writers drop the leading space of blank context lines.
			oldLines = append(oldLines, "")
			newLines = append(newLines, "")
		case section == "update":
			switch line[0] {
			case '-':
				oldLines = append(oldLines, line[1:])
			case '+':
				newLines = append(newLines, line[1:])
			case ' ':
				oldLines = append(oldLines, line[1:])
				newLines = append(newLines, line[1:])
			}
		}
	}
	flushHunk()
	if files != 1 {
		return other
	}
	if op == "add" {
		return write(path, strings.Join(added, "\n"))
	}
	return edit(path, edits)
}

func exec(command string) Action {
	if command == "" {
		return other
	}
	return Action{Kind: KindExec, Command: command}
}

func read(path string, offset, limit int) Action {
	if path == "" {
		return other
	}
	return Action{Kind: KindRead, Path: path, Offset: offset, Limit: limit}
}

func write(path, content string) Action {
	if path == "" {
		return other
	}
	return Action{Kind: KindWrite, Path: path, Content: content}
}

func edit(path string, edits []Edit) Action {
	if path == "" || len(edits) == 0 {
		return other
	}
	return Action{Kind: KindEdit, Path: path, Edits: edits}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
