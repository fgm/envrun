package main

import (
	"os"
	"runtime"
	"testing"
)

// TestIssue9ErrorHandling tests the fix for issue #9: panic when dereferencing
// nil exit pointer on non-exit errors. Verifies both non-exit errors and actual
// exit errors are handled without panic.
//
// Uses cross-platform commands to work on Windows, macOS, and Linux.
func TestIssue9ErrorHandling(t *testing.T) {
	// Define cross-platform commands
	var (
		exitErrorArgs  []string
		nonExistentCmd string
	)

	if runtime.GOOS == "windows" {
		exitErrorArgs = []string{"cmd", "/c", "exit 42"}
		nonExistentCmd = "nonexistent_command_12345.exe"
	} else {
		exitErrorArgs = []string{"sh", "-c", "exit 42"}
		nonExistentCmd = "nonexistent_command_12345"
	}

	tests := []struct {
		name string
		args []string
		desc string
	}{
		{
			name: "non-exit error",
			args: []string{nonExistentCmd},
			desc: "command not found should not panic",
		},
		{
			name: "exit error",
			args: exitErrorArgs,
			desc: "actual exit error should not panic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inline setup: create temporary .env file
			envFile, err := os.CreateTemp("", ".env")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(envFile.Name())

			if _, err := envFile.WriteString("TEST_VAR=test_value\n"); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			envFile.Close()

			// Setup test args and restore original args after test
			originalArgs := os.Args
			defer func() { os.Args = originalArgs }()

			testArgs := []string{"envrun", "-f", envFile.Name()}
			testArgs = append(testArgs, tt.args...)
			os.Args = testArgs

			// Verify no panic occurs (issue #9 fix)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Unexpected panic in %s: %v", tt.desc, r)
				}
			}()

			// Call main() which should handle errors gracefully
			main()
		})
	}
}
