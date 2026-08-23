package env_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/fgm/envrun/env"
)

// TestProblemError pins the rendered form of every kind.
//
// The messages are an output contract — the command prints them and the README
// quotes their shape — so a change to the wording should fail here first and say
// which one it broke, rather than surfacing as a mismatched substring elsewhere.
func TestProblemError(t *testing.T) {
	tests := []struct {
		name     string
		problem  env.Problem
		expected string
	}{
		{
			name:     "a line with no equals sign names no name",
			problem:  env.Problem{Line: 2, Err: env.ErrNotAPair},
			expected: "line 2: not a name=value pair",
		},
		{
			name:     "an invalid name is quoted, since it may hold spaces",
			problem:  env.Problem{Line: 7, Err: env.ErrInvalidName, Name: "SPA CED"},
			expected: `line 7: invalid name "SPA CED"`,
		},
		{
			// The name is carried but not printed: the value is what holds the
			// NUL, and the value never reaches a message. See Problem.
			name:     "a NUL in a value names neither the value nor the name",
			problem:  env.Problem{Line: 3, Err: env.ErrNUL, Name: "BAD"},
			expected: "line 3: value contains NUL",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := test.problem.Error(); actual != test.expected {
				t.Errorf("Error() = %q, expected %q", actual, test.expected)
			}
			if !errors.Is(test.problem, test.problem.Err) {
				t.Errorf("Problem does not unwrap to %v", test.problem.Err)
			}
		})
	}
}

// TestParseErrorIs covers the question a caller asks without knowing ParseError
// exists: did this file fail for this reason?
//
// It is the half errors.As cannot serve, since a file rejected for several
// reasons wraps several problems and As stops at the first.
func TestParseErrorIs(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		is      error
		isNot   error
	}{
		{
			name:    "an invalid name is reachable through the file's error",
			fixture: "fail-name-with-space",
			is:      env.ErrInvalidName,
			isNot:   env.ErrNotAPair,
		},
		{
			name:    "so is a line that is not a pair",
			fixture: "fail-no-separator",
			is:      env.ErrNotAPair,
			isNot:   env.ErrNUL,
		},
		{
			name:    "and a NUL in a value",
			fixture: "fail-nul-value",
			is:      env.ErrNUL,
			isNot:   env.ErrInvalidName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := env.Load(filepath.Join("testdata", test.fixture+".env"))
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !errors.Is(err, test.is) {
				t.Errorf("errors.Is(err, %v) is false for %v", test.is, err)
			}
			// The other half: a sentinel the file did not trip must not match,
			// or asking the question tells the caller nothing.
			if errors.Is(err, test.isNot) {
				t.Errorf("errors.Is(err, %v) is true for %v", test.isNot, err)
			}
		})
	}
}

// TestParseErrorWrapsEveryProblem covers a file failing for more than one reason.
//
// One problem per Unwrap would be indistinguishable from the single-problem case
// above, so this is what proves the []error form is doing anything: both kinds
// are reachable from the one error.
func TestParseErrorWrapsEveryProblem(t *testing.T) {
	perr := &env.ParseError{
		Path: "several.env",
		Problems: []env.Problem{
			{Line: 2, Err: env.ErrNotAPair},
			{Line: 5, Err: env.ErrInvalidName, Name: "9LEADING"},
		},
	}

	for _, sentinel := range []error{env.ErrNotAPair, env.ErrInvalidName} {
		if !errors.Is(perr, sentinel) {
			t.Errorf("errors.Is(err, %v) is false, so only one problem is reachable", sentinel)
		}
	}
	if errors.Is(perr, env.ErrNUL) {
		t.Error("errors.Is(err, ErrNUL) is true for a file that holds no NUL")
	}
	expected := `invalid environment file several.env: line 2: not a name=value pair; line 5: invalid name "9LEADING"`
	if actual := perr.Error(); actual != expected {
		t.Errorf("Error() = %q, expected %q", actual, expected)
	}
}

// TestCloseError covers the one Note that is also an error: it must satisfy the
// Note interface for Result to carry it, and keep its cause reachable so a
// caller can tell a permission failure from any other.
func TestCloseError(t *testing.T) {
	cause := &fs.PathError{Op: "close", Path: "x.env", Err: fs.ErrPermission}
	n := env.CloseError{Err: cause}

	var note env.Note = n
	expected := "could not close the environment file: close x.env: permission denied"
	if actual := note.String(); actual != expected {
		t.Errorf("String() = %q, expected %q", actual, expected)
	}

	// The property Stringer alone would not give: the cause survives.
	err, ok := note.(error)
	if !ok {
		t.Fatal("CloseError does not satisfy error, so its cause is unreachable")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("errors.Is(err, fs.ErrPermission) is false for %v", err)
	}
}
