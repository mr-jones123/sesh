package handoff

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/mr-jones123/sesh/internal/tools"
)

// ResumeCommand is display text, not an executable shell script used by Launch.
func (r Result) ResumeCommand() string {
	args := make([]string, len(r.Command))
	for i, arg := range r.Command {
		args[i] = tools.ShellQuote(arg)
	}
	return "cd " + tools.ShellQuote(r.Workspace) + " && " + strings.Join(args, " ")
}

func Launch(ctx context.Context, result Result, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(result.Command) == 0 {
		return fmt.Errorf("no target command")
	}
	cmd := exec.CommandContext(ctx, result.Command[0], result.Command[1:]...)
	cmd.Dir = result.Workspace
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("target exited: %w; native session kept at %s\nresume with: %s", err, result.Path, result.ResumeCommand())
	}
	return nil
}
