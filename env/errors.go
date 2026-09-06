package env

import (
	"errors"
	"fmt"
	"strings"
)

// Why a line can be rejected.
//
// Sentinels rather than an enum, so a caller asks the question it actually has -
// errors.Is(err, env.ErrInvalidName) -
// without needing to know that [Problem] or [ParseError] exist,
// let alone how to walk them.
// [ParseError] wraps every problem it collected,
// so errors.Is reaches all of them, not just the first.
//
// Their text is the whole message bar the line number, so the rendered form and
// the sentinel cannot drift apart.
var (
	// ErrInvalidName reports a name outside the accepted set: a letter or an
	// underscore, then letters, digits, dots, dashes or underscores.
	ErrInvalidName = errors.New("invalid name")
	// ErrNotAPair reports a line holding no "=" at all.
	ErrNotAPair = errors.New("not a name=value pair")
	// ErrNUL reports a value holding a NUL, which execve cannot carry.
	ErrNUL = errors.New("value contains NUL")
	// ErrTooLong reports a line the scanner cannot hold whole. It is a fault in
	// the file rather than a failure to read it, so it is a problem like the
	// others: truncating the line would corrupt a value silently.
	ErrTooLong = errors.New("too long")
)

// CloseError reports that the environment file could not be closed once it had
// been read.
//
// It is a [Note] rather than an error returned from [Load], because everything
// the file had to give has already been read: refusing to run over it would
// withhold a working environment for a problem that no longer affects it.
//
// It is also an error, the one note that genuinely reports a failure, so the
// cause - an *io/fs.PathError, which names the file itself - stays reachable:
//
//	if e, ok := n.(error); ok && errors.Is(e, fs.ErrPermission) { ... }
type CloseError struct {
	// Err is what Close returned.
	Err error
}

func (e CloseError) String() string { return e.Error() }

func (e CloseError) Error() string {
	return fmt.Sprintf("could not close the environment file: %v", e.Err)
}

func (e CloseError) Unwrap() error { return e.Err }

// Problem locates one rejected line.
//
// It carries the line number and, where there was one to read, the name -
// and never the value.
// A malformed line may hold a secret, and a caller printing what it is handed
// must not be able to print that secret by accident.
type Problem struct {
	// Line is 1-based, as an editor counts.
	Line int
	// Err is one of [ErrNotAPair], [ErrInvalidName], [ErrNUL] or [ErrTooLong].
	Err error
	// Name is the offending name for [ErrInvalidName],
	// the name whose value was refused for [ErrNUL],
	// and empty for [ErrNotAPair] and [ErrTooLong],
	// where no name could be read from the line.
	Name string
}

func (p Problem) Error() string {
	// The name is shown only where it is the fault itself. For ErrNUL it is the
	// value that is at fault, and the value never reaches a message.
	if errors.Is(p.Err, ErrInvalidName) {
		return fmt.Sprintf("line %d: %v %q", p.Line, p.Err, p.Name)
	}
	return fmt.Sprintf("line %d: %v", p.Line, p.Err)
}

func (p Problem) Unwrap() error { return p.Err }

// ParseError reports a file rejected as a whole, and why, line by line.
//
// Every problem in the file is collected before it is returned,
// so one run names every line at fault rather than the first.
//
// A rejection is fatal, unlike a [Result.Notes] entry,
// because envrun is the only thing that reads the file:
// a line it cannot honour would have reached nothing without it,
// so passing that line over leaves the command running without the
// configuration the operator meant to set - worse than not running it at all.
//
// [FromEnviron] does the opposite with the same shape of row,
// and the difference is the baseline rather than the format:
// an inherited row would have reached the command anyway.
//
// Two ways in, because callers want two different things. To ask whether a kind
// of problem occurred at all:
//
//	if errors.Is(err, env.ErrInvalidName) { ... }
//
// To count them, or to locate them in the file:
//
//	if perr, ok := errors.AsType[*env.ParseError](err); ok {
//		for _, p := range perr.Problems { ... }
//	}
//
// The second is what [Unwrap] cannot serve: errors.As stops at the first match
// in the tree, so a caller enumerating through it would have to walk the
// multi-error interface by hand.
type ParseError struct {
	// Path is the file the problems were found in.
	Path     string
	Problems []Problem
}

func (e *ParseError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Error()
	}
	// Joined on one line rather than with errors.Join's newlines: these reach a
	// caller through a single log line beginning "envrun failed:", and a
	// multi-line error would put the lines after the first outside that contract.
	return fmt.Sprintf("invalid environment file %s: %s", e.Path, strings.Join(parts, "; "))
}

// Unwrap exposes every problem to errors.Is and errors.As.
//
// The []error form is what errors.Join produces, and what the errors package
// walks: returning it directly gives the same reach without giving up the
// fields [ParseError] carries or the single-line message above.
func (e *ParseError) Unwrap() []error {
	errs := make([]error, len(e.Problems))
	for i, p := range e.Problems {
		errs[i] = p
	}
	return errs
}
