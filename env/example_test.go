// The examples are a separate package from the tests beside them, so godoc
// renders them as a reader can paste them: env.Apply rather than a bare Apply.
package env_test

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/fgm/envrun/env"
)

// writeEnv puts body in a file of its own, returning its path and a function to
// remove it. Examples call it so that they read as being about envrun rather
// than about temporary files.
//
// They write their own file rather than reading testdata, because only go test
// runs a binary with the package directory as its working directory. An example
// is also compiled as a standalone program — by pkg.go.dev's Run button, say —
// where a relative path would resolve against somewhere else entirely.
func writeEnv(body string) (path string, remove func()) {
	dir, err := os.MkdirTemp("", "envrun")
	if err != nil {
		log.Fatal(err)
	}
	path = filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		log.Fatal(err)
	}
	return path, func() { os.RemoveAll(dir) }
}

// ExampleApply is the importer's entry point: one call at startup, before
// anything concurrent begins, and the variables are in the process.
func ExampleApply() {
	path, remove := writeEnv("FRESH=fromfile\nTAKEN=fromfile\nBRACED=${FRESH}\n")
	defer remove()

	// The state a parent shell, or an earlier line of main, would leave behind:
	// TAKEN already set, the other two not.
	os.Setenv("TAKEN", "frominherited")
	os.Unsetenv("FRESH")
	os.Unsetenv("BRACED")

	res, err := env.Apply(path)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%-18s %s\n", "file read:", filepath.Base(res.Path))
	fmt.Printf("%-18s %-14s(was unset, so the file's value applies)\n", "FRESH:", os.Getenv("FRESH"))
	fmt.Printf("%-18s %-14s(was already set, so the environment wins)\n", "TAKEN:", os.Getenv("TAKEN"))
	fmt.Printf("%-18s %-14s(never expanded: the file is read, not sourced)\n", "BRACED:", os.Getenv("BRACED"))
	fmt.Printf("%-18s %-14s(what the file declared, kept though it lost)\n", "Result.Env[TAKEN]:", res.Env["TAKEN"])
	// Output:
	// file read:         .env
	// FRESH:             fromfile      (was unset, so the file's value applies)
	// TAKEN:             frominherited (was already set, so the environment wins)
	// BRACED:            ${FRESH}      (never expanded: the file is read, not sourced)
	// Result.Env[TAKEN]: fromfile      (what the file declared, kept though it lost)
}

// ExampleParseError shows the two ways into a rejected file, which answer
// different questions: whether a kind of problem occurred at all, and where
// every one of them is.
func ExampleParseError() {
	path, remove := writeEnv("GOOD=1\nSPA CED=bad\nNOEQUALS\n")
	defer remove()

	_, err := env.Load(path)

	// Did this kind of problem occur? A sentinel reaches every problem the file
	// produced, not only the first.
	fmt.Println("an invalid name:", errors.Is(err, env.ErrInvalidName))
	fmt.Println("a NUL value:    ", errors.Is(err, env.ErrNUL))

	// Where are they? Every line at fault is collected before the error is
	// returned, so one run names them all.
	if perr, ok := errors.AsType[*env.ParseError](err); ok {
		fmt.Println("file:", filepath.Base(perr.Path))
		for _, p := range perr.Problems {
			fmt.Printf("  line %d: %v (name %q)\n", p.Line, p.Err, p.Name)
		}
	}
	// Output:
	// an invalid name: true
	// a NUL value:     false
	// file: .env
	//   line 2: invalid name (name "SPA CED")
	//   line 3: not a name=value pair (name "")
}
