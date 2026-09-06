// This file is in package env, where every other test file is in env_test,
// because the property it covers is unreachable from outside: Load opens the
// file itself, read-only, and a descriptor with no write-back has nothing left
// to fail at on Close. loadFile takes a reader precisely so a fake can fail.
package env

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
)

// failingCloser is an opened environment file whose Close reports err.
type failingCloser struct {
	io.Reader
	err error
}

func (f failingCloser) Close() error { return f.err }

// TestLoadFileCloseError covers what a failed close must not cost the caller:
// everything the file had to give has already been read, so the close failure
// travels as a Note beside the result rather than replacing it.
func TestLoadFileCloseError(t *testing.T) {
	const path = "x.env"
	cause := &fs.PathError{Op: "close", Path: path, Err: fs.ErrPermission}

	t.Run("a usable file", func(t *testing.T) {
		rc := failingCloser{Reader: strings.NewReader("NAME=value\n"), err: cause}

		res, err := loadFile(rc, path)
		if err != nil {
			t.Fatalf("error = %v, expected none: a close failure must not withhold a working environment", err)
		}
		if res.Env["NAME"] != "value" {
			t.Errorf("Env = %v, expected it to hold NAME=value", res.Env)
		}
		assertCloseNote(t, res, path, cause)
	})

	t.Run("a rejected file", func(t *testing.T) {
		rc := failingCloser{Reader: strings.NewReader("9LEADING=x\n"), err: cause}

		res, err := loadFile(rc, path)
		perr, ok := errors.AsType[*ParseError](err)
		if !ok {
			t.Fatalf("error %v is not a *ParseError, so a caller cannot act on it", err)
		}
		if perr.Path != path {
			t.Errorf("ParseError.Path = %q, expected %q", perr.Path, path)
		}
		if res.Env != nil {
			t.Errorf("expected no environment beside an error, got %v", res.Env)
		}
		// The property this case exists for: the parse failure does not bury the
		// unrelated finding the caller could still act on.
		assertCloseNote(t, res, path, cause)
	})
}

func assertCloseNote(t *testing.T, res Result, path string, cause error) {
	t.Helper()

	if res.Path != path {
		t.Errorf("Result.Path = %q, expected %q", res.Path, path)
	}
	if len(res.Notes) != 1 {
		t.Fatalf("Notes = %v, expected exactly the close failure", res.Notes)
	}
	note, ok := res.Notes[0].(CloseError)
	if !ok {
		t.Fatalf("note %v is not a CloseError, so its cause is unreachable", res.Notes[0])
	}
	if !errors.Is(note, cause) {
		t.Errorf("errors.Is(note, cause) is false for %v", note)
	}
}
