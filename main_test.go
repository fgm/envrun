package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fgm/envrun/env"
)

// The roles this binary can take when it is not running tests.
//
// Several cases need a command that reports something about the process it runs in,
// and re-entering this binary is the cheapest such command available.
// Dispatching in TestMain keeps that out of the test list:
// the alternative idiom, a Test function guarded by a skip,
// reports a skipped test on every ordinary run for something that is not a test.
//
// helperVar reaches the helper through the environment *file*, never the inherited environment:
// envrun passes its own environment on,
// so a helper role set on the envrun process would be taken by that process instead of by its command.
// mainVar takes the opposite route for the same reason —
// it must reach envrun and, were the helper switch not first, it would reach the helper too.
const (
	helperVar = "ENVRUN_TEST_HELPER"
	mainVar   = "ENVRUN_TEST_MAIN"
	outVar    = "ENVRUN_TEST_OUT"
)

func TestMain(m *testing.M) {
	switch os.Getenv(helperVar) {
	case "dump":
		helperReport(strings.Join(os.Environ(), "\n"))
	case "pid":
		helperReport(strconv.Itoa(os.Getpid()))
	case "cat":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "helper process:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv(mainVar) != "" {
		// os.Args is a plain envrun command line here: see envrunCmd.
		os.Exit(realMain(os.Args))
	}
	os.Exit(m.Run())
}

// helperReport writes the one fact the helper was asked for, and ends the process.
func helperReport(s string) {
	if err := os.WriteFile(os.Getenv(outVar), []byte(s), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "helper process:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// writeEnvFile writes an environment file holding the given rows, and returns its path.
func writeEnvFile(t *testing.T, rows ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	body := strings.Join(append(rows, ""), "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed writing environment file: %v", err)
	}
	return path
}

// envrunCmd prepares this test binary to run as envrun in a subprocess.
//
// Every case where the command actually starts needs one:
// on *nix realMain hands the process over with execve,
// so calling it in process would replace the test binary with the command under test.
// The pre-exec failures need no subprocess,
// and stay in process where they still count towards coverage.
func envrunCmd(t *testing.T, envFile string, command ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	cmd := exec.Command(exe, append([]string{"-f", envFile}, command...)...)
	cmd.Env = append(os.Environ(), mainVar+"=1")
	return cmd
}

// shell names an interpreter able to exit with a chosen status on this platform.
func shell() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/c"
	}
	return "sh", "-c"
}

// failPrefix is what fail() writes,
// and what the README tells a caller to look for
// when a 125, 126 or 127 could have come from either envrun or the command.
// Asserted from both sides —
// present for every own failure, absent whenever the command ran —
// since a rule documented as exact is worth nothing if only half of it is checked.
const failPrefix = "envrun failed: "

// TestDiagnosticsNameEnvrun states the attribution rule at the level it is written.
//
// The tests around it check the rule end to end,
// where a diagnostic could be missing for any number of reasons;
// this one checks the two lines themselves,
// so a change to their wording fails here first and says which contract it broke.
// note() is the half with no *nix caller in the tests —
// its only one is a close failure —
// and the half a Windows regression would show up in.
func TestDiagnosticsNameEnvrun(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	}()

	fail(errors.New("boom"))
	if actual := buf.String(); actual != failPrefix+"boom\n" {
		t.Errorf("fail() wrote %q, expected %q", actual, failPrefix+"boom\n")
	}

	buf.Reset()
	note("%s exited with status %d", "sh", 42)
	actual := buf.String()
	if expected := "envrun: sh exited with status 42\n"; actual != expected {
		t.Errorf("note() wrote %q, expected %q", actual, expected)
	}
	// The load-bearing half:
	// envrun did not fail, so the line must not read as though it had,
	// or a caller attributes the command's status to envrun.
	if strings.Contains(actual, failPrefix) {
		t.Errorf("note() claims a failure: %q", actual)
	}
}

// TestCommandExitStatus covers the statuses a command that actually ran reports.
//
// It guards issue #36, where a failing command exited 0 and masked the failure.
// Under exec these are not envrun's statuses to report at all:
// the command is the process,
// so what is asserted is that nothing stands between it and its caller.
func TestCommandExitStatus(t *testing.T) {
	sh, flag := shell()
	envFile := writeEnvFile(t, "TEST_VAR=test_value")

	tests := []struct {
		name     string
		script   string
		expected int
	}{
		{name: "success returns 0", script: "exit 0", expected: 0},
		{name: "issue 36: failure returns the command's own status", script: "exit 42", expected: 42},
		{name: "other non-zero status is reported as-is", script: "exit 1", expected: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := envrunCmd(t, envFile, sh, flag, test.script)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			// A non-zero status is the point here,
			// so only a failure to start at all —
			// leaving no ProcessState behind — is a test failure.
			if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
				t.Fatalf("failed running envrun: %v", err)
			}
			if actual := cmd.ProcessState.ExitCode(); actual != test.expected {
				t.Errorf("exit status = %d, expected %d", actual, test.expected)
			}
			// The other half of the attribution the README documents:
			// these statuses are the command's, so envrun must not claim any of them,
			// including the 125-to-127 range where the two are indistinguishable.
			if strings.Contains(stderr.String(), failPrefix) {
				t.Errorf("stderr claims an envrun failure for a command that ran: %q", stderr.String())
			}
		})
	}
}

// TestRealMainOwnFailures covers the statuses envrun reports for its own failures.
//
// These are the only statuses left to it once the command starts,
// and they run in process because none of them reaches the command.
// Issue #9 is guarded here too:
// a non-exit error used to panic on a nil pointer,
// exiting 2 and breaking the documented contract.
// The classification cases that turn on a POSIX errno are in main_unix_test.go,
// since that is where they have a defined answer.
func TestRealMainOwnFailures(t *testing.T) {
	sh, flag := shell()
	valid := writeEnvFile(t, "TEST_VAR=test_value")

	tests := []struct {
		name    string
		envFile string
		command []string
		// cause identifies the failure the case is about.
		// Four of these exit 125, so the status alone cannot tell them apart:
		// without it, moving a check between parseArgs and env.Load
		// would leave every case passing while proving something else.
		// Kept to text both platforms produce, since Windows runs this too.
		cause    string
		expected int
	}{
		{
			name:     "missing environment file returns 125",
			envFile:  filepath.Join(t.TempDir(), "absent"),
			command:  []string{sh, flag, "exit 0"},
			cause:    "absent: open",
			expected: ExitEnvrun,
		},
		{
			name:     "a line that is not a name=value pair fails the file, returning 125",
			envFile:  writeEnvFile(t, "not a pair"),
			command:  []string{sh, flag, "exit 0"},
			cause:    "line 1: not a name=value pair",
			expected: ExitEnvrun,
		},
		{
			name:     "an unknown flag returns 125",
			envFile:  valid,
			command:  []string{"-nosuchflag", sh, flag, "exit 0"},
			cause:    "parsing flags: flag provided but not defined: -nosuchflag",
			expected: ExitEnvrun,
		},
		{
			name:     "no command to run returns 125",
			envFile:  valid,
			command:  nil,
			cause:    "no command to run",
			expected: ExitEnvrun,
		},
		{
			name: "a line too long to hold returns 125",
			// Over bufio.Scanner's 64 KiB token limit. The fault is in the file,
			// so it is reported as a problem naming its line, not as a read
			// failure: see env.ErrTooLong.
			envFile:  writeEnvFile(t, "TOO_LONG="+strings.Repeat("x", 64*1024)),
			command:  []string{sh, flag, "exit 0"},
			cause:    "line 1: too long",
			expected: ExitEnvrun,
		},
		{
			name:     "issue 9: an unknown command returns 127 without panicking",
			envFile:  valid,
			command:  []string{"nonexistent_command_12345"},
			cause:    "running nonexistent_command_12345",
			expected: ExitNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("unexpected panic: %v", r)
				}
			}()

			var stderr bytes.Buffer
			log.SetOutput(&stderr)
			defer log.SetOutput(os.Stderr)

			args := append([]string{"envrun", "-f", test.envFile}, test.command...)
			if actual := realMain(args); actual != test.expected {
				t.Errorf("realMain() = %d, expected %d", actual, test.expected)
			}
			if !strings.Contains(stderr.String(), failPrefix) {
				t.Errorf("stderr does not attribute the failure to envrun: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), test.cause) {
				t.Errorf("failed for the wrong reason: %q does not mention %q", stderr.String(), test.cause)
			}
		})
	}
}

// TestRealMainNeedsSomethingToRun covers the one check that happens before the
// flags are parsed at all.
//
// The table above cannot reach it: every case there builds "-f <file>" into the
// arguments, so the length test always passes and the failure comes later, from
// an empty flags.Args() instead.
func TestRealMainNeedsSomethingToRun(t *testing.T) {
	var stderr bytes.Buffer
	log.SetOutput(&stderr)
	defer log.SetOutput(os.Stderr)

	if actual := realMain([]string{"envrun"}); actual != ExitEnvrun {
		t.Errorf("realMain() = %d, expected %d", actual, ExitEnvrun)
	}
	if !strings.Contains(stderr.String(), failPrefix) {
		t.Errorf("stderr does not attribute the failure to envrun: %q", stderr.String())
	}
}

// TestCommandReadsStandardInput covers issue #1: a filter command gets its input.
//
// envrun never wired standard input, so anything reading it saw EOF.
// Under exec the streams are the process's own and there is nothing to wire;
// on Windows they are assigned to the child
// rather than piped through a copying goroutine.
func TestCommandReadsStandardInput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "unused")
	envFile := writeEnvFile(t, helperVar+"=cat", outVar+"="+out)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}

	cmd := envrunCmd(t, envFile, exe)
	cmd.Stdin = strings.NewReader("x\n")
	cmd.Stderr = os.Stderr
	dumped, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the filter: %v", err)
	}
	if got := string(dumped); got != "x\n" {
		t.Errorf("the command read %q from standard input, expected %q", got, "x\n")
	}
}

// TestCommandGetsOpaqueRows covers the rows envrun cannot represent
// reaching the command.
//
// env.FromEnviron classifying them proves nothing on its own:
// what matters is the environment the command is given,
// and deleting the append that carries them left the rest of this suite green.
//
// The command is this test binary rather than a shell, because a shell rebuilds
// its own environment before exec'ing anything, and would be free to drop the very
// rows under test. See helperVar.
func TestCommandGetsOpaqueRows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the environment rows under test cannot occur on Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	out := filepath.Join(t.TempDir(), "env.txt")
	envFile := writeEnvFile(t, helperVar+"=dump", outVar+"="+out)
	opaque := []string{"NOEQUALS", "=novalue"}

	cmd := envrunCmd(t, envFile, exe)
	cmd.Env = append(cmd.Env, opaque...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the helper: %v", err)
	}

	dumped, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the environment the command saw: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(dumped), "\n"), "\n")
	for _, want := range opaque {
		if !slices.Contains(got, want) {
			t.Errorf("the command's environment lacks %q; it holds %q", want, got)
		}
	}
}

// TestReportNotes covers the reporting of Result.Notes, which no end-to-end case
// can reach: the only note envrun raises needs a failing Close, and no portable
// test can provoke one. Handing it a fabricated note exercises the same loop.
func TestReportNotes(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	}()

	cause := &fs.PathError{Op: "close", Path: "x.env", Err: fs.ErrPermission}
	reportNotes([]env.Note{env.CloseError{Err: cause}})

	// A note, not a failure: envrun did not fail, so the line must not say so.
	expected := "envrun: could not close the environment file: close x.env: permission denied\n"
	if actual := buf.String(); actual != expected {
		t.Errorf("reportNotes() wrote %q, expected %q", actual, expected)
	}

	// Nothing to report writes nothing at all.
	buf.Reset()
	reportNotes(nil)
	if actual := buf.String(); actual != "" {
		t.Errorf("reportNotes(nil) wrote %q, expected nothing", actual)
	}
}
