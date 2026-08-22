package main

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRealMainExitStatus covers the exit status realMain reports.
//
// It guards issue #36, where a failing command exited 0 and masked the failure,
// and issue #9, where a non-exit error panicked on a nil pointer.
// It exercises realMain rather than main,
// because main exits the process and would take the test binary with it.
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

// TestEnvFromReader covers parsing, over the fixtures in testdata.
//
// Each fixture isolates one behaviour,
// because a single file cannot exercise more than one rejection:
// the first failure is reported for the whole file, hiding anything after it.
func TestEnvFromReader(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		expect  env
		errored string
	}{
		{
			name:    "values are transported, never interpreted",
			fixture: "pass-literal-values",
			expect: env{
				"HASH":       "sharp after a var # is not a comment",
				"SQUOTED":    "'quoted'",
				"DQUOTED":    `"dquoted"`,
				"BACKQUOTED": "`backquoted`",
				"MISQUOTED":  `"misquoted'`,
				"JSON":       `{"a": "b"}`,
			},
		},
		{
			name:    "no interpolation of any dollar form",
			fixture: "pass-no-interpolation",
			expect: env{
				"SIMPLE": "simple",
				"BRACED": "${SIMPLE}",
				"BARE":   "$SIMPLE",
				"DOLLAR": "p$ssw0rd",
			},
		},
		{
			name:    "names may hold dots, dashes and a leading underscore",
			fixture: "pass-valid-names",
			expect: env{
				"PLAIN": "1", "lower": "2", "DOT.TED": "3",
				"DASH-ED": "4", "_LEADING": "5", "MIX_ed.Na-me9": "6",
			},
		},
		{
			name:    "only the first equals sign separates",
			fixture: "pass-equals-in-value",
			expect: env{
				"ASSIGN": "foo=bar",
				"URL":    "postgres://h:5432/db?sslmode=disable",
			},
		},
		{
			name:    "comments and blank lines are skipped",
			fixture: "pass-comments-blanks",
			expect:  env{"KEPT": "yes"},
		},
		{
			name:    "blanks around the name and the value are trimmed",
			fixture: "pass-trimmed",
			expect: env{
				"SPACED":       "value with spaces inside",
				"TABBED":       "tabbed",
				"INDENTED":     "leading spaces",
				"TAB_INDENTED": "leading tab",
				"BOTH":         "both sides",
			},
		},
		{
			name:    "a repeated name keeps the last value, as Merge does",
			fixture: "pass-duplicate-name",
			expect: env{
				"DUP":   "second",
				"OTHER": "x",
			},
		},
		{
			name:    "a name holding a space is refused",
			fixture: "fail-name-with-space",
			errored: `line 2: invalid name "SPA CED"`,
		},
		{
			name:    "a name opening on a digit is refused",
			fixture: "fail-leading-digit",
			errored: `line 2: invalid name "9LEADING"`,
		},
		{
			name:    "a name outside ASCII is refused",
			fixture: "fail-non-ascii-name",
			errored: `line 2: invalid name "Aé"`,
		},
		{
			name:    "a line without an equals sign is refused",
			fixture: "fail-no-separator",
			errored: "line 2: not a name=value pair",
		},
		{
			name:    "a value spanning lines is refused, not truncated",
			fixture: "fail-multiline",
			errored: "line 3: not a name=value pair",
		},
		{
			name:    "the shell export prefix is refused, this format is not sourceable",
			fixture: "fail-export-prefix",
			errored: `line 2: invalid name "export EXPORTED"`,
		},
	}

	// The prefix is load-bearing: the Makefile builds the demo from pass-*.env,
	// so a fixture named for the wrong outcome would quietly break the demo.
	covered := make(map[string]bool, len(tests))
	for _, test := range tests {
		covered[test.fixture] = true
		wantErr := test.errored != ""
		if got := strings.HasPrefix(test.fixture, "fail-"); got != wantErr {
			t.Errorf("fixture %q: name says failing=%v, case expects failing=%v",
				test.fixture, got, wantErr)
		}
	}
	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.env"))
	if err != nil {
		t.Fatalf("failed listing fixtures: %v", err)
	}
	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".env")
		if !covered[name] {
			t.Errorf("fixture %q has no case: every file in testdata must be asserted", name)
		}
	}
	// Both checks above report every problem before this stops the test:
	// running the cases on an inconsistent fixture set buries the real failure
	// under a list of passing subtests.
	if t.Failed() {
		t.FailNow()
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, err := os.Open(filepath.Join("testdata", test.fixture+".env"))
			if err != nil {
				t.Fatalf("failed opening fixture: %v", err)
			}
			defer file.Close()

			actual, err := envFromReader(file)
			if test.errored != "" {
				if err == nil {
					t.Fatalf("expected an error holding %q, got none", test.errored)
				}
				if !strings.Contains(err.Error(), test.errored) {
					t.Errorf("error = %q, expected it to hold %q", err, test.errored)
				}
				if actual != nil {
					t.Errorf("expected no environment beside an error, got %v", actual)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !maps.Equal(actual, test.expect) {
				t.Errorf("environment = %v, expected %v", actual, test.expect)
			}
		})
	}
}
