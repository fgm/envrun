package env

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"maps"
	"os"
	"regexp"
	"strings"
)

const (
	// AppName is the name of the application, regardless of its invocation.
	AppName = "envrun"

	// commentRxS matches comment lines.
	commentRxS = `^[\s]*#`
	// nameRxS is much tighter than Posix, which accepts anything but NUL and '=',
	// but laxer than shells, which do not accept dots. Names are assumed to be pre-trimmed.
	// The leading underscore shells allow is accepted too: _JAVA_OPTIONS and its kind are real.
	nameRxS = `^[_a-zA-Z][-._a-zA-Z0-9]*$`
)

var (
	commentRx = regexp.MustCompile(commentRxS)
	nameRx    = regexp.MustCompile(nameRxS)
)

// DefaultPath is where [Load] and [Apply] look when given no path of their own.
const DefaultPath = ".env"

// Vars is a set of environment variables, as the file declared them or as a
// process inherited them.
//
// It is a map because nothing here needs an order:
// execve takes an array the kernel does not order,
// and the one place order matters - reporting - can sort at presentation time,
// where the caller knows what it is sorting for.
type Vars map[string]string

// Note is a non-fatal finding: something [Load] observed that did not stop it.
//
// It is a [fmt.Stringer] rather than an error, because most notes report
// nothing that failed. A repeated name is the coming case, and a file holding
// one is valid: the format resolves it last-wins, which is why its fixture is
// named pass-duplicate-name.env. Typing that as an error would have the API
// contradict the parser.
//
// A note that does report a failure implements error as well,
// so its cause stays reachable through errors.Is and errors.As.
// [CloseError] is the one such note today.
//
// A caller needing more than the text discriminates by type,
// which is why the implementations are exported.
//
// Like [Problem], a note may name a variable and the lines involved, never a value.
type Note fmt.Stringer

// Result is everything [Load] observed, for a caller to present as it likes.
type Result struct {
	// Path is the file actually used, which discovery makes worth reporting:
	// with several candidates, the caller cannot otherwise tell which one won.
	Path string

	// Env is what the file declared, and never the merge with the inherited
	// environment.
	//
	// The merged view would be redundant: after applying, os.Environ is that merge.
	// The file's own set is the part that cannot be recovered afterwards,
	// an applied variable being indistinguishable from an inherited one.
	//
	// That distinction is what #39 needs: -clean hands the set to the command
	// as its whole environment, where the default merges it under the inherited one.
	Env Vars

	// Notes are non-fatal findings, in the order they were seen, and are
	// returned even beside an error. A failed close is the only one so far;
	// a repeated name, silently overwritten today, is the next. See #3.
	Notes []Note
}

// Merge combines two sets of variables.
//
// If names overlap, the argument wins over the receiver, as in PHP array_merge.
// The command's merge is fileEnv.Merge(inherited), so the inherited environment
// overrides the file - see PR #6 for the request to reverse that.
func (v Vars) Merge(w Vars) Vars {
	res := make(Vars, len(v)+len(w))
	maps.Copy(res, v)
	maps.Copy(res, w)
	return res
}

// FromEnviron parses environment rows as os.Environ returns them,
// returning the pairs it can represent and, separately, the rows it cannot.
//
// envp is a plain array at the execve level:
// the kernel enforces neither shape nor uniqueness,
// so any parent can hand us a row that is not name=value,
// and indexing a split on the assumption that it is one used to panic.
// Two kinds arrive, and they are not the same thing despite being handled alike:
//
//   - a row without "=", which no name can match,
//     since getenv compares a name against the text before the "=";
//   - a row with an empty name, such as "=value", which getenv("") does find.
//
// Both are passed through rather than dropped,
// because a command run without envrun would see them.
// Only the first is genuinely unreadable.
//
// Dedup keeps the first occurrence, as getenv does when scanning envp.
// It never fires on os.Environ output, which syscall.copyenv has already deduped,
// but the rows are the caller's to supply,
// and the map must not impose the opposite rule on them.
func FromEnviron(rows []string) (Vars, []string) {
	v := make(Vars)
	var opaque []string
	for _, row := range rows {
		k, val, found := strings.Cut(row, "=")
		if !found || k == "" {
			opaque = append(opaque, row)
			continue
		}
		if _, seen := v[k]; seen {
			continue
		}
		v[k] = val
	}
	return v, opaque
}

// Environ renders the variables as execve takes them: a plain array of rows.
// It is the inverse of [FromEnviron], and opaque is what that returned second.
//
// The rows envrun could not represent follow the pairs
// rather than keeping their original position,
// since a map has no order to preserve them in.
func (v Vars) Environ(opaque []string) []string {
	rows := make([]string, 0, len(v)+len(opaque))
	for k, val := range v {
		rows = append(rows, fmt.Sprintf("%s=%s", k, val))
	}
	return append(rows, opaque...)
}

// Export sets the variables into this process, and is what [Apply] does with
// what it read.
//
// A name already set in the process is left alone, which is the same precedence
// the command applies: it merges the file under the inherited environment, so
// the inherited value wins. A variable the caller exported therefore survives
// the call.
//
// It mutates process state, with the consequences for concurrent readers set out
// in [Apply].
//
// It is separate from [Apply] so that a caller who took [Load] to avoid the
// mutation can still choose it afterwards, rather than writing the loop itself.
func (v Vars) Export() error {
	for k, val := range v {
		if _, set := os.LookupEnv(k); set {
			continue
		}
		// Reachable only through a hand-built Vars: os.Setenv rejects an empty
		// name and one holding "=" or a NUL, and nameRxS refuses all three, so
		// nothing [Load] produces can reach this.
		if err := os.Setenv(k, val); err != nil {
			return fmt.Errorf("setting %s: %w", k, err)
		}
	}
	return nil
}

// parseReader parses an environment file, collecting every problem it finds.
//
// Every line must be blank, a comment, or name=value with a valid name;
// anything else is a [Problem], and one problem rejects the file. See [ParseError].
//
// It also rejects two things this format cannot carry:
//
//   - a value containing a NUL, which cannot be passed to a command at all;
//   - a multiline quoted value, which other formats allow. envrun does not
//     interpret quotes, so a value spanning lines cannot be represented, and
//     keeping only its first line would be corruption rather than an omission.
//
// A repeated name keeps its last value rather than failing,
// matching both the usual expectation for this format and [Vars.Merge],
// where the later set wins.
// It is the one silent overwriting left here, and worth a [Result.Notes] entry
// once something can carry one: see #3.
//
// The returned error is a read failure alone, where the file has no line at
// fault to name. A line the scanner cannot hold is not one of those: it is a
// [Problem] carrying its line number, since the fault is in the file.
func parseReader(r io.Reader) (Vars, []Problem, error) {
	v := make(Vars)
	var problems []Problem
	scanner := bufio.NewScanner(r)
	line := 1
	for ; scanner.Scan(); line++ {
		row := scanner.Text()
		if strings.TrimSpace(row) == "" || commentRx.MatchString(row) {
			continue
		}
		k, val, found := strings.Cut(row, "=")
		if !found {
			problems = append(problems, Problem{Line: line, Err: ErrNotAPair})
			continue
		}
		k = strings.Trim(k, " \t")
		val = strings.Trim(val, " \t")
		if !nameRx.MatchString(k) {
			problems = append(problems, Problem{Line: line, Err: ErrInvalidName, Name: k})
			continue
		}
		// A NUL cannot reach a command: exec rejects it before execve,
		// and execve takes NUL-terminated strings in any case.
		// Letting it through already failed, and already exited 125.
		// What catching it here adds is the line number,
		// and an error attributed to the file rather than to the command.
		if strings.ContainsRune(val, 0) {
			problems = append(problems, Problem{Line: line, Err: ErrNUL, Name: k})
			continue
		}
		v[k] = val
	}
	if err := scanner.Err(); err != nil {
		// A line too long to hold is a fault in the file, like a NUL or a bad
		// name, not a failure to read it: it joins the problems so that it
		// carries a line number, and so that recognising it needs no knowledge
		// of which scanner this package happens to use.
		if !errors.Is(err, bufio.ErrTooLong) {
			return nil, nil, fmt.Errorf("reading the environment file: %w", err)
		}
		problems = append(problems, Problem{Line: line, Err: ErrTooLong})
	}
	return v, problems, nil
}

// Load finds an environment file, reads it, and applies nothing.
//
// This is the command's entry point: envrun must not put the variables into its
// own process, because what it needs is the set to hand to the command it runs.
// An importer wants [Apply] instead.
//
// paths is a search path, not a list to merge: the first candidate that exists
// wins, the rest are never read, and composition - .env then .env.local - stays
// a separate feature rather than a second meaning for this parameter. With no
// path at all it looks for [DefaultPath] in the working directory. The command's
// -f flag overrides the search by naming its one candidate.
//
// The error says which half of the job failed:
//
//   - the file's contents, as a [*ParseError],
//     whose problems are reachable one by one with errors.AsType;
//   - reaching the file at all, as an *io/fs.PathError.
//     errors.Is against fs.ErrNotExist then separates "no file" -
//     which may be no error at all for a caller with an optional one -
//     from a file that is there but could not be read.
//
// A failing Result is not empty: [Result.Path] and [Result.Notes] are returned
// alongside the error whenever a file was opened at all. Only [Result.Env] is
// nil, since a rejected file declares nothing.
func Load(paths ...string) (Result, error) {
	file, path, err := openFirst(paths)
	if err != nil {
		return Result{}, err
	}
	return loadFile(file, path)
}

// openFirst opens the first candidate that exists, and reports which one that was:
// with just a search path, the caller could not tell.
//
// The file is returned open, and closing it is the caller's job from here.
func openFirst(paths []string) (*os.File, string, error) {
	if len(paths) == 0 {
		paths = []string{DefaultPath}
	}

	var lastErr error
	for _, candidate := range paths {
		f, err := os.Open(candidate)
		if err == nil {
			return f, candidate, nil
		}
		// Only a missing file is a miss.
		// A candidate that exists but cannot be read - no permission, a directory -
		// is a problem to report rather than a reason to look further:
		// silently falling through to the next candidate would hide it.
		if !errors.Is(err, iofs.ErrNotExist) {
			return nil, "", fmt.Errorf("reading %s: %w", candidate, err)
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("reading %s: %w", strings.Join(paths, ", "), lastErr)
}

// loadFile reads an opened environment file, closes it,
// and reports what it declared.
// path names the file, for the Result and any [ParseError] to carry.
//
// It takes an interface where its only caller holds an *os.File,
// because the close it has to report on is the one thing a real file will not do:
// a descriptor opened read-only has nothing left to fail at.
func loadFile(rc io.ReadCloser, path string) (Result, error) {
	v, problems, err := parseReader(rc)
	// Closed here rather than deferred: the command hands over with syscall.Exec,
	// which runs no deferred function, so this package must not leave the close
	// to one either.
	//
	// A failed close is a Note rather than an error: everything the file had to
	// give has already been read, so refusing to run the command over it would
	// withhold a working environment for a problem that no longer affects it.
	var notes []Note
	if cErr := rc.Close(); cErr != nil {
		notes = append(notes, CloseError{Err: cErr})
	}
	// Path and Notes survive a failure, and only Env does not: a file that was
	// found and read has already produced findings worth reporting, and losing
	// them to an unrelated parse failure would hide the one thing the caller
	// could still act on.
	if err != nil {
		return Result{Path: path, Notes: notes}, err
	}
	if len(problems) > 0 {
		return Result{Path: path, Notes: notes}, &ParseError{Path: path, Problems: problems}
	}
	return Result{Path: path, Env: v, Notes: notes}, nil
}

// Apply is [Load] followed by [Vars.Export].
//
// This is the importer's entry point, and the one line the library exists to
// spare them: an API that returned the merge-and-apply loop to its caller would
// not be simplifying anything.
//
// It mutates the process state, so it belongs at the top of main, or in TestMain,
// before anything concurrent starts.
// Go-to-Go access is safe on its own, since syscall.Setenv and Getenv share a
// mutex. What that mutex does not reach is:
//
//   - A C library calling getenv on another thread;
//   - Any reader that captured a value before the change and never learns of it.
//
// There is no concurrency-safe way to mutate a process environment.
// Callers who need one want [Load], which touches nothing
// and hands back the same Result.
//
// The precedence is [Vars.Export]'s: a name already set in the process is left
// alone, so a variable the caller exported survives the call.
func Apply(paths ...string) (Result, error) {
	res, err := Load(paths...)
	if err != nil {
		return res, err
	}
	return res, res.Env.Export()
}
