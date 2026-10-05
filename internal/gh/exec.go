package gh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExecRunner runs the process directly, without a shell.
type ExecRunner struct{}

// Run starts argv with the parent environment plus env and returns stdout.
// On failure the error carries the child's stderr.
func (ExecRunner) Run(ctx context.Context, argv, env []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, errors.New("no command to run")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%q not found on PATH (gh_command runs without a shell, so shell aliases and functions do not work; use `env VAR=value gh` or gh_env instead)", argv[0])
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return nil, fmt.Errorf("%s: %s", argv[0], msg)
	}
	return nil, fmt.Errorf("%s: %w", argv[0], err)
}
