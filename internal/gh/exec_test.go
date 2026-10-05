package gh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestHelperProcess is not a real test. The tests below run the test binary
// itself as a stand-in for gh, so the runner is checked against a real child
// process without needing gh installed.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("PRLEDGER_HELPER") != "1" {
		return
	}
	switch os.Getenv("PRLEDGER_HELPER_MODE") {
	case "fail":
		fmt.Fprintln(os.Stderr, "gh: not logged in")
		os.Exit(4)
	default:
		fmt.Printf("probe=%s", os.Getenv("PRLEDGER_PROBE"))
		os.Exit(0)
	}
}

func helperArgv() []string {
	return []string{os.Args[0], "-test.run=^TestHelperProcess$"}
}

func TestExecRunnerPassesEnvToTheChild(t *testing.T) {
	t.Setenv("PRLEDGER_HELPER", "1")
	out, err := ExecRunner{}.Run(context.Background(), helperArgv(), []string{"PRLEDGER_PROBE=from-gh_env"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "probe=from-gh_env" {
		t.Fatalf("stdout = %q", got)
	}
}

// gh_command may be `env VAR=value gh`; env is a real program, so this works
// without a shell.
func TestExecRunnerSupportsEnvPrefixCommand(t *testing.T) {
	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env not available")
	}
	t.Setenv("PRLEDGER_HELPER", "1")
	argv := append([]string{envPath, "PRLEDGER_PROBE=from-gh_command"}, helperArgv()...)
	out, err := ExecRunner{}.Run(context.Background(), argv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "probe=from-gh_command" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestExecRunnerReportsStderrOnFailure(t *testing.T) {
	t.Setenv("PRLEDGER_HELPER", "1")
	_, err := ExecRunner{}.Run(context.Background(), helperArgv(), []string{"PRLEDGER_HELPER_MODE=fail"})
	if err == nil || !strings.Contains(err.Error(), "gh: not logged in") {
		t.Fatalf("err = %v, want the child's stderr", err)
	}
}

func TestExecRunnerExplainsMissingProgram(t *testing.T) {
	_, err := ExecRunner{}.Run(context.Background(), []string{"with-gh-personal", "gh"}, nil)
	if err == nil || !strings.Contains(err.Error(), "aliases") {
		t.Fatalf("err = %v, want a hint that shell aliases do not work", err)
	}
}
