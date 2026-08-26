//go:build unix

package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/fgm/envrun/env"
)

// TestExecKeepsThePID covers the process replacement itself,
// which is what issue #35 asks for.
//
// The repro in that issue — kill envrun and watch the command survive —
// cannot be asserted directly without racing on whether an orphan is still around.
// Identity can:
// after execve the command *is* the process that was envrun,
// so its PID is the one the caller started.
// A wrapper cannot make that true, whatever it forwards.
func TestExecKeepsThePID(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	out := filepath.Join(t.TempDir(), "pid")
	envFile := writeEnvFile(t, helperVar+"=pid", outVar+"="+out)

	cmd := envrunCmd(t, envFile, exe)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the helper: %v", err)
	}

	reported, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the PID the command ran under: %v", err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(reported)))
	if err != nil {
		t.Fatalf("parsing the reported PID: %v", err)
	}
	if want := cmd.Process.Pid; got != want {
		t.Errorf("the command ran as PID %d, expected %d — envrun is still a wrapper", got, want)
	}
}

// TestExecReportsTheCommandsSignalDeath covers the other half of the same claim.
//
// A command killed by a signal leaves a wait status saying so,
// and under exec that status is the one the caller collects:
// there is no wrapper to translate it into an exit code.
// This is why the 128+signo scheme the issue proposed is moot rather than unimplemented.
// See ADR-001.
func TestExecReportsTheCommandsSignalDeath(t *testing.T) {
	envFile := writeEnvFile(t, "TEST_VAR=test_value")

	cmd := envrunCmd(t, envFile, "sh", "-c", "kill -TERM $$")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("the command was expected to die of SIGTERM, but exited cleanly")
	}

	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("wait status is a %T, expected a syscall.WaitStatus", cmd.ProcessState.Sys())
	}
	if !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Errorf("wait status = %v, expected death by SIGTERM", cmd.ProcessState)
	}
}

// TestRealMainErrnoClassification covers the statuses that turn on a POSIX errno.
//
// They live here rather than beside the other pre-exec failures
// because only *nix defines the answers:
// Windows resolves a command by extension and has no execute bit,
// so neither EISDIR nor ENOEXEC arises there.
//
// EISDIR is the one that bites.
// exec.LookPath reports it for a directory,
// and errors.Is(EISDIR, fs.ErrPermission) is false,
// so without its own case in exitStatus a directory reports 125
// where both the wrapper envrun used to be and bash report 126.
func TestRealMainErrnoClassification(t *testing.T) {
	valid := writeEnvFile(t, "TEST_VAR=test_value")

	// notExecutable returns the path of a file which exists but carries no execute bit.
	notExecutable := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "not-executable")
		if err := os.WriteFile(path, []byte("not a binary\n"), 0o600); err != nil {
			t.Fatalf("failed writing non-executable file: %v", err)
		}
		return path
	}

	// notABinary returns the path of an executable file which execve cannot load:
	// no shebang, and not an object file.
	// Shells retry these under /bin/sh;
	// os/exec never did, and neither does exec.
	notABinary := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "not-a-binary")
		if err := os.WriteFile(path, []byte("echo nope\n"), 0o700); err != nil {
			t.Fatalf("failed writing non-binary file: %v", err)
		}
		return path
	}

	tests := []struct {
		name     string
		command  func(*testing.T) string
		expected int
	}{
		{
			name:     "a command without the execute bit returns 126",
			command:  notExecutable,
			expected: ExitNotInvocable,
		},
		{
			name:     "EISDIR: a directory as the command returns 126, not 125",
			command:  func(t *testing.T) string { return t.TempDir() },
			expected: ExitNotInvocable,
		},
		{
			name:     "ENOEXEC: an executable execve cannot load returns 126",
			command:  notABinary,
			expected: ExitNotInvocable,
		},
		{
			// EINVAL: a NUL cannot appear in a path, and LookPath reports that
			// as neither ErrNotFound nor a permission error, so it reaches the
			// default arm. The name holds a slash so LookPath stats it rather
			// than searching $PATH, where it would be a plain "not found".
			name:     "an errno the switch does not name returns 125",
			command:  func(t *testing.T) string { return "./nul\x00in-name" },
			expected: ExitEnvrun,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Captured as the sibling table in main_test.go does.
			// Left to the default logger these four lines reach the real standard
			// error, where they read as failures of the run rather than as the
			// deliberate ones they are, and carry a timestamp no real run has,
			// main() being the only caller of log.SetFlags.
			var stderr bytes.Buffer
			log.SetOutput(&stderr)
			defer log.SetOutput(os.Stderr)

			if actual := realMain([]string{env.AppName, "-f", valid, test.command(t)}); actual != test.expected {
				t.Errorf("realMain() = %d, expected %d", actual, test.expected)
			}
			// Asserted rather than merely swallowed: muting the line without
			// reading it would lose the diagnostic and check nothing in its place.
			// Every status here is envrun's own, so every one must say so.
			if !strings.Contains(stderr.String(), failPrefix) {
				t.Errorf("stderr does not attribute the failure to envrun: %q", stderr.String())
			}
		})
	}
}

// TestBashOnNotInvocable pins the 126 expectations above to the shell they came from.
//
// The statuses envrun reports for a command it could not start
// are a convention borrowed from coreutils,
// so the only thing that can tell us we have drifted from it
// is asking a shell the same question.
// Skipped where bash is absent rather than approximated with sh,
// whose statuses for these cases are not the same contract.
//
// The shebang-less script is the deliberate divergence, and is asserted as one:
// bash retries such a file under /bin/sh and runs it,
// where execve reports ENOEXEC and envrun stops at 126.
// envrun transports an environment, it does not choose an interpreter —
// but a change on either side should fail here rather than silently.
func TestBashOnNotInvocable(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is not installed: %v", err)
	}
	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(notExecutable, []byte("not a binary\n"), 0o600); err != nil {
		t.Fatalf("failed writing non-executable file: %v", err)
	}
	notABinary := filepath.Join(dir, "not-a-binary")
	if err := os.WriteFile(notABinary, []byte("echo nope\n"), 0o700); err != nil {
		t.Fatalf("failed writing non-binary file: %v", err)
	}

	tests := []struct {
		name     string
		command  string
		expected int
	}{
		{name: "a directory", command: dir, expected: ExitNotInvocable},
		{name: "a file without the execute bit", command: notExecutable, expected: ExitNotInvocable},
		{name: "a shebang-less script: bash retries it under sh, envrun reports 126", command: notABinary, expected: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(bash, "-c", `exec "$0" >/dev/null 2>&1`, test.command)
			_ = cmd.Run()
			if got := cmd.ProcessState.ExitCode(); got != test.expected {
				t.Errorf("bash exec = %d, expected %d: the convention has moved", got, test.expected)
			}
		})
	}
}
