package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRealMainExitStatus covers the exit status realMain reports.
//
// It guards issue #36, where a failing command exited 0 and masked the
// failure, and issue #9, where a non-exit error panicked on a nil pointer.
// It exercises realMain rather than main, because main exits the process
// and would take the test binary with it.
func TestRealMainExitStatus(t *testing.T) {
	shell, shellFlag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/c"
	}

	// envFile writes a valid environment file and returns its path.
	envFile := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte("TEST_VAR=test_value\n"), 0o600); err != nil {
			t.Fatalf("failed writing environment file: %v", err)
		}
		return path
	}

	// notExecutable returns the path of a file which exists but cannot be run.
	notExecutable := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "not-executable")
		if err := os.WriteFile(path, []byte("not a binary\n"), 0o600); err != nil {
			t.Fatalf("failed writing non-executable file: %v", err)
		}
		return path
	}

	tests := []struct {
		name     string
		envFile  func(*testing.T) string
		command  []string
		expected int
	}{
		{
			name:     "success returns 0",
			envFile:  envFile,
			command:  []string{shell, shellFlag, "exit 0"},
			expected: 0,
		},
		{
			name:     "issue 36: failure returns the child exit status",
			envFile:  envFile,
			command:  []string{shell, shellFlag, "exit 42"},
			expected: 42,
		},
		{
			name:     "other non-zero status is reported as-is",
			envFile:  envFile,
			command:  []string{shell, shellFlag, "exit 1"},
			expected: 1,
		},
		{
			name:     "issue 9: non-exit error returns without panicking",
			envFile:  envFile,
			command:  []string{"nonexistent_command_12345"},
			expected: ExitNotFound,
		},
		{
			name:     "command not executable returns 126",
			envFile:  envFile,
			command:  []string{notExecutable(t)},
			expected: ExitNotInvocable,
		},
		{
			name:     "missing environment file returns 125",
			envFile:  func(*testing.T) string { return filepath.Join(t.TempDir(), "absent") },
			command:  []string{shell, shellFlag, "exit 0"},
			expected: ExitEnvrun,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("unexpected panic: %v", r)
				}
			}()

			args := append([]string{"envrun", "-f", test.envFile(t)}, test.command...)
			if actual := realMain(args); actual != test.expected {
				t.Errorf("realMain() = %d, expected %d", actual, test.expected)
			}
		})
	}
}
