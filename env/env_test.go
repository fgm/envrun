package env_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/fgm/envrun/env"
)

// TestLoad covers parsing, over the fixtures in testdata.
//
// Each fixture isolates one behaviour, so that a failure names one cause.
// The parser does accumulate problems and reports them together,
// so a fixture could carry several — it just would not say which one broke.
func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		expect  env.Vars
		errored string
		// problems is what errors.As must reach.
		// errored above pins the rendered message, which is an output contract;
		// this pins the structure a caller acts on without matching text.
		problems []env.Problem
	}{
		{
			name:    "values are transported, never interpreted",
			fixture: "pass-literal-values",
			expect: env.Vars{
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
			expect: env.Vars{
				"SIMPLE": "simple",
				"BRACED": "${SIMPLE}",
				"BARE":   "$SIMPLE",
				"DOLLAR": "p$ssw0rd",
			},
		},
		{
			name:    "names may hold dots, dashes and a leading underscore",
			fixture: "pass-valid-names",
			expect: env.Vars{
				"PLAIN": "1", "lower": "2", "DOT.TED": "3",
				"DASH-ED": "4", "_LEADING": "5", "MIX_ed.Na-me9": "6",
			},
		},
		{
			name:    "only the first equals sign separates",
			fixture: "pass-equals-in-value",
			expect: env.Vars{
				"ASSIGN": "foo=bar",
				"URL":    "postgres://h:5432/db?sslmode=disable",
			},
		},
		{
			name:    "comments and blank lines are skipped",
			fixture: "pass-comments-blanks",
			expect:  env.Vars{"KEPT": "yes"},
		},
		{
			name:    "blanks around the name and the value are trimmed",
			fixture: "pass-trimmed",
			expect: env.Vars{
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
			expect: env.Vars{
				"DUP":   "second",
				"OTHER": "x",
			},
		},
		{
			name:     "a name holding a space is refused",
			fixture:  "fail-name-with-space",
			errored:  `line 2: invalid name "SPA CED"`,
			problems: []env.Problem{{Line: 2, Err: env.ErrInvalidName, Name: "SPA CED"}},
		},
		{
			name:     "a name opening on a digit is refused",
			fixture:  "fail-leading-digit",
			errored:  `line 2: invalid name "9LEADING"`,
			problems: []env.Problem{{Line: 2, Err: env.ErrInvalidName, Name: "9LEADING"}},
		},
		{
			name:     "a name outside ASCII is refused",
			fixture:  "fail-non-ascii-name",
			errored:  `line 2: invalid name "Aé"`,
			problems: []env.Problem{{Line: 2, Err: env.ErrInvalidName, Name: "Aé"}},
		},
		{
			name:     "a line without an equals sign is refused",
			fixture:  "fail-no-separator",
			errored:  "line 2: not a name=value pair",
			problems: []env.Problem{{Line: 2, Err: env.ErrNotAPair}},
		},
		{
			name:     "a value spanning lines is refused, not truncated",
			fixture:  "fail-multiline",
			errored:  "line 3: not a name=value pair",
			problems: []env.Problem{{Line: 3, Err: env.ErrNotAPair}},
		},
		{
			name:     "the shell export prefix is refused, this format is not sourceable",
			fixture:  "fail-export-prefix",
			errored:  `line 2: invalid name "export EXPORTED"`,
			problems: []env.Problem{{Line: 2, Err: env.ErrInvalidName, Name: "export EXPORTED"}},
		},
		{
			name:     "a value holding a NUL is refused, execve could not carry it",
			fixture:  "fail-nul-value",
			errored:  "line 2: value contains NUL",
			problems: []env.Problem{{Line: 2, Err: env.ErrNUL, Name: "BAD"}},
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
			path := filepath.Join("testdata", test.fixture+".env")

			res, err := env.Load(path)
			if test.errored != "" {
				if err == nil {
					t.Fatalf("expected an error holding %q, got none", test.errored)
				}
				if !strings.Contains(err.Error(), test.errored) {
					t.Errorf("error = %q, expected it to hold %q", err, test.errored)
				}
				if res.Env != nil {
					t.Errorf("expected no environment beside an error, got %v", res.Env)
				}
				// A failing Result still names the file it read.
				if res.Path != path {
					t.Errorf("Result.Path = %q, expected %q even beside an error", res.Path, path)
				}
				perr, ok := errors.AsType[*env.ParseError](err)
				if !ok {
					t.Fatalf("error %v is not a *ParseError, so a caller cannot act on it", err)
				}
				if perr.Path != path {
					t.Errorf("ParseError.Path = %q, expected %q", perr.Path, path)
				}
				if !slices.Equal(perr.Problems, test.problems) {
					t.Errorf("problems = %v, expected %v", perr.Problems, test.problems)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Path != path {
				t.Errorf("Result.Path = %q, expected %q", res.Path, path)
			}
			if !maps.Equal(res.Env, test.expect) {
				t.Errorf("environment = %v, expected %v", res.Env, test.expect)
			}
		})
	}
}

// TestFromEnviron covers what a parent can hand us, but os.Setenv cannot produce.
//
// An environment entry without "=" is legal at the execve level,
// and splitting one on the assumption that it is a pair used to panic with an index error,
// exiting 2 and breaking the documented 125/126/127 contract.
func TestFromEnviron(t *testing.T) {
	rows := []string{"A=1", "NOEQUALS", "=novalue", "B=2", "C=", "A=2"}
	// A repeated name keeps the first, as getenv does when scanning envp.
	want := env.Vars{"A": "1", "B": "2", "C": ""}
	wantOpaque := []string{"NOEQUALS", "=novalue"}

	got, gotOpaque := env.FromEnviron(rows)

	if !slices.Equal(gotOpaque, wantOpaque) {
		t.Errorf("FromEnviron() opaque = %q, want %q", gotOpaque, wantOpaque)
	}

	if len(got) != len(want) {
		t.Fatalf("FromEnviron() got %d entries, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if g, ok := got[k]; !ok || g != w {
			t.Errorf("FromEnviron()[%q] = %q, %t; want %q, true", k, g, ok, w)
		}
	}
}

// TestLoadDiscovery covers paths as a search path rather than a list to merge.
//
// The first candidate that exists wins and the rest are never read, which is
// what makes -f an override rather than one more entry: the command hands Load
// a single candidate. See ADR-002.
func TestLoadDiscovery(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.env")
	second := filepath.Join(dir, "second.env")
	absent := filepath.Join(dir, "absent.env")
	for path, body := range map[string]string{first: "WHICH=first\n", second: "WHICH=second\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing a fixture: %v", err)
		}
	}

	tests := []struct {
		name  string
		paths []string
		// won names the candidate expected to be read, and "" expects none.
		won string
	}{
		{name: "a single candidate is read", paths: []string{first}, won: first},
		{name: "the first existing candidate wins", paths: []string{first, second}, won: first},
		{name: "a missing candidate is skipped, not fatal", paths: []string{absent, second}, won: second},
		{name: "no candidate at all", paths: []string{absent}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := env.Load(test.paths...)
			if test.won == "" {
				// Distinguishable from a rejected file: a caller whose file is
				// optional treats "absent" as no error and "malformed" as one.
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("error = %v, expected it to wrap fs.ErrNotExist", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Path != test.won {
				t.Errorf("Result.Path = %q, expected %q", res.Path, test.won)
			}
		})
	}
}

// TestLoadCandidateThatCannotBeRead covers the failure discovery must not swallow.
//
// Only a missing candidate is a miss. One that exists but cannot be read is a
// problem to report: falling through to the next would leave the operator
// running against a file they did not mean to use, with nothing said about the
// one they did. So the assertion is that an error comes back at all — reaching
// the fallback is exactly what returns nil.
//
// Neither case uses a mode: root ignores the permission bits, and CI images run
// tests as root often enough for that to matter.
func TestLoadCandidateThatCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "fallback.env")
	regular := filepath.Join(dir, "regular")
	for path, body := range map[string]string{fallback: "WHICH=fallback\n", regular: "not a directory\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing a fixture: %v", err)
		}
	}

	tests := []struct {
		name string
		// candidate is tried before fallback, which exists and parses.
		candidate string
		windows   bool
	}{
		// A directory opens without complaint on Unix; it is the read that
		// fails. The branch differs from the one below, the property does not.
		{name: "a directory", candidate: dir, windows: true},
		// ENOTDIR: a path whose parent is a regular file. This is the one that
		// fails at the open. Windows reports it as a missing path instead,
		// which would make it a legitimate miss rather than this case.
		{name: "a path under a regular file", candidate: filepath.Join(regular, ".env")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.windows && runtime.GOOS == "windows" {
				t.Skip("Windows reports this as a missing path, which is a miss rather than a failure")
			}
			res, err := env.Load(test.candidate, fallback)
			if err == nil {
				t.Fatalf("expected an error, got Result.Path = %q: discovery fell through", res.Path)
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("error = %v, expected something other than a missing file", err)
			}
		})
	}
}

// TestLoadDefaultPath covers Load called with no candidate of its own.
func TestLoadDefaultPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, env.DefaultPath), []byte("WHICH=default\n"), 0o600); err != nil {
		t.Fatalf("writing a fixture: %v", err)
	}
	t.Chdir(dir)

	res, err := env.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Path != env.DefaultPath {
		t.Errorf("Result.Path = %q, expected %q", res.Path, env.DefaultPath)
	}
	if actual := res.Env["WHICH"]; actual != "default" {
		t.Errorf("Env[WHICH] = %q, expected %q", actual, "default")
	}
}

// TestLoadLineTooLong covers a file with no line at fault to name.
//
// Over bufio.Scanner's 64 KiB token limit, so the read fails rather than the
// parse, and the result is a plain error rather than a *ParseError.
func TestLoadLineTooLong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.env")
	body := "TOO_LONG=" + strings.Repeat("x", 64*1024) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing a fixture: %v", err)
	}

	_, err := env.Load(path)
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	// The fault is in the file, so it arrives as a problem naming its line,
	// not as a failure to read wrapping bufio's sentinel.
	if !errors.Is(err, env.ErrTooLong) {
		t.Errorf("error = %q, expected it to be ErrTooLong", err)
	}
	perr, ok := errors.AsType[*env.ParseError](err)
	if !ok {
		t.Fatalf("error = %q, expected a *ParseError", err)
	}
	if len(perr.Problems) != 1 {
		t.Fatalf("Problems = %v, expected exactly one", perr.Problems)
	}
	if actual := perr.Problems[0].Line; actual != 1 {
		t.Errorf("Line = %d, expected the line that was too long", actual)
	}
	// bufio is an implementation detail a caller must not need to know.
	if strings.Contains(err.Error(), "bufio") {
		t.Errorf("error = %q, expected it not to name the scanner", err)
	}
}

// TestApply covers the applying half, and the precedence it shares with the command.
//
// The command merges the file under the inherited environment, so the inherited
// value wins; Apply must not do the opposite, or the same file would mean two
// different things depending on which delivery read it.
func TestApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "LOAD_FRESH=fromfile\nLOAD_TAKEN=fromfile\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing a fixture: %v", err)
	}
	t.Setenv("LOAD_TAKEN", "frominherited")

	res, err := env.Apply(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actual := os.Getenv("LOAD_FRESH"); actual != "fromfile" {
		t.Errorf("LOAD_FRESH = %q, expected the file's value", actual)
	}
	if actual := os.Getenv("LOAD_TAKEN"); actual != "frominherited" {
		t.Errorf("LOAD_TAKEN = %q, expected the inherited value to win", actual)
	}
	// Result.Env is what the file declared, never the merge:
	// the applied variable must still be reported with the file's own value,
	// which is the part os.Environ cannot recover afterwards.
	if actual := res.Env["LOAD_TAKEN"]; actual != "fromfile" {
		t.Errorf("Result.Env[LOAD_TAKEN] = %q, expected the file's value", actual)
	}
}

// TestApplyFailsWithLoad covers Apply adding nothing where Load rejected the file.
func TestApplyFailsWithLoad(t *testing.T) {
	if _, err := env.Apply(filepath.Join("testdata", "fail-no-separator.env")); err == nil {
		t.Fatal("expected an error, got none")
	}
}

// TestVarsMerge covers the argument winning, which is the direction the whole
// precedence question turns on. See PR #6.
func TestVarsMerge(t *testing.T) {
	receiver := env.Vars{"ONLY_LEFT": "l", "BOTH": "l"}
	argument := env.Vars{"ONLY_RIGHT": "r", "BOTH": "r"}
	expected := env.Vars{"ONLY_LEFT": "l", "ONLY_RIGHT": "r", "BOTH": "r"}

	actual := receiver.Merge(argument)
	if !maps.Equal(actual, expected) {
		t.Errorf("Merge() = %v, expected %v", actual, expected)
	}
	// A new map, so neither operand is disturbed:
	// the command merges into what it then hands to execve, and a receiver
	// mutated in passing would change what Result.Env reports.
	if _, mutated := receiver["ONLY_RIGHT"]; mutated {
		t.Errorf("Merge() wrote into its receiver: %v", receiver)
	}
}

// TestVarsEnviron covers the round trip FromEnviron and Environ make, opaque
// rows included: those cannot survive the map, so only the pair proves they last.
func TestVarsEnviron(t *testing.T) {
	rows := []string{"A=1", "B=", "NOEQUALS", "=novalue"}

	v, opaque := env.FromEnviron(rows)
	actual := v.Environ(opaque)

	// Sorted because a map has no order, which is the whole reason Environ
	// cannot keep the opaque rows in their original position.
	slices.Sort(actual)
	expected := slices.Sorted(slices.Values(rows))
	if !slices.Equal(actual, expected) {
		t.Errorf("Environ() = %q, expected %q", actual, expected)
	}
}

// TestVarsExport covers Export used on its own, which is the only way to reach
// os.Setenv's error: the accepted name set refuses every name os.Setenv
// rejects, so nothing Load produces can carry one.
func TestVarsExport(t *testing.T) {
	const fresh, taken = "EXPORT_FRESH", "EXPORT_TAKEN"
	t.Setenv(taken, "frominherited")
	t.Cleanup(func() { os.Unsetenv(fresh) })

	if err := (env.Vars{fresh: "fromvars", taken: "fromvars"}).Export(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actual := os.Getenv(fresh); actual != "fromvars" {
		t.Errorf("%s = %q, expected the exported value", fresh, actual)
	}
	if actual := os.Getenv(taken); actual != "frominherited" {
		t.Errorf("%s = %q, expected the inherited value to win", taken, actual)
	}

	// A name holding "=" is one os.Setenv rejects and only a hand-built Vars can
	// hold, parsing having refused it.
	const secret = "s3cr3t-value"
	err := env.Vars{"BAD=NAME": secret}.Export()
	if err == nil {
		t.Fatal("expected an error for a name holding \"=\", got none")
	}
	if !strings.Contains(err.Error(), "BAD=NAME") {
		t.Errorf("Export() error = %q, expected it to name the variable", err)
	}
	// The invariant SECURITY.md documents, at the one site where a value is in
	// scope when a message is built.
	if strings.Contains(err.Error(), secret) {
		t.Errorf("Export() error = %q, expected it to withhold the value", err)
	}
}
