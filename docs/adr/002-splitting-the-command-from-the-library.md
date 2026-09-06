# ADR-002. Splitting envrun into a command and a library

- Status: Accepted
- Date: 2026-08-23
- Ticket: #43 - split out of #35 on 2026-08-23
- Scope: the package layout and the shape of the library API.
  How the command is handed over is ADR-001.

## Context

envrun is a command.
Its goal, recorded in ADR-001 - run a command "as if it had the environment internally" -
has a second half that the command form cannot serve: a Go program should be able to obtain the same environment *internally*, with no wrapper process at all.

```go
res, err := env.Apply() // that is the whole of it
```

**Why not simply reach for `joho/godotenv` in the Go code, and envrun in CI?**
Because the two read the same file differently.
godotenv *accepts* what envrun rejects - interpolation, quoted multiline values, lax names -
so a team using both would have its CI and its tests reading different dialects of one file,
and finding out only when they disagreed.
The single core is what makes *the same file means the same thing* a promise rather than a slogan,
and envrun's deliberate narrowness is what makes the promise keepable:
a format that interprets less has less to disagree about.
That parity is the library's whole value proposition.

Before the PR implementing this ADR, loading, parsing, merging,
and the CLI all lived in `package main`, so none of it was importable.
The split is what makes the second delivery possible; it is not a tidying exercise.

## Options and findings

### Layout

- `github.com/fgm/envrun` - the CLI stays at the root,
  because "the CLI version is likely to be typed more times than the import path".
- `github.com/fgm/envrun/env` - load, parse, merge, apply.

A third package, `github.com/fgm/envrun/autoload`, was examined and declined:
see *Rejected: an `autoload` package*.

### Library shape

#### What the shape has to satisfy

Problems are *returned rather than logged*:
a library cannot know what its caller's output has to look like,
so presenting them is the caller's job.
Returning them flattened into one string is not enough either -
a caller must be able to count them, locate them in the file,
and act on one without matching on a message.

##### Rejected: any logger parameter

Three shapes were considered, and they fail for different reasons,
so the argument has to be made three times.

- **`io.Writer`** fails on formatting rather than plumbing:
  text written beneath a `slog.JSONHandler` interleaves non-JSON lines into a JSON stream,
  and anything parsing that output breaks. Same for logfmt.
- **`*log.Logger`** is *not* disposed of by that argument,
  and it is worth being explicit that the interleaving objection reaches no further than `io.Writer`.
  A slog application can pass `slog.NewLogLogger(handler, level)` and get clean,
  correctly framed records.
  What it loses is the structure it wanted: the whole message arrives as one `msg` string,
  so the JSON consumer - the very caller the first objection was about -
  is worse off than if it had been handed the data.
  `slog.NewLogLogger(handler, level)` also takes its level *once*, at
  construction, and a `*log.Logger` has no notion of level to vary it with -
  so every message the library sends arrives at the one severity the caller
  chose when building the bridge, and filtering cannot separate one kind of
  finding from another. That costs nothing while the only output is Notes;
  it is the shape foreclosing something rather than present harm.
- **`*slog.Logger`** answers both of those -
  structure is preserved, levels are per-call - and fails on reach instead.
  It is a concrete struct, not an interface,
  and it cannot be derived from an existing logger: the bridge runs the other way,
  `slog.NewLogLogger` producing a `*log.Logger` from a handler,
  with nothing coming back.
  So a caller already on zap, logrus or apex must either adopt `slog`
  or write a `slog.Handler` wrapping their own logger - a dependency,
  and a decision envrun would be imposing on them.
  The standard library declines this itself:
  `http.Server.ErrorLog` and `httputil.ReverseProxy.ErrorLog` are still
  `*log.Logger`, six releases after `slog` shipped.

The three approaches trade reach against fidelity, in opposite directions,
and none of them dominates:

- `io.Writer` accepts every caller and carries nothing,
- `*slog.Logger` carries everything and accepts almost no one,
- `*log.Logger` sits in between.

That is the shape of a false choice, and returning the data escapes it -
the library already hands back everything it observed.

Hence `Result.Notes` carries the non-fatal findings and `ParseError` the fatal ones,
so every caller gets full fidelity at no adoption cost, whatever it logs with.
A logger parameter would be a second channel for the same information,
leaving the caller to work out which one to read.
Presentation belongs to the only party that knows the required format,
which is never the library.

#### Chosen shape: return the data, and let the caller present it

- `Result.Path` - which file was actually used.
- `Result.Env` - what the file declared, never the merge.
  The merged view would be redundant: the file's set is merged *under* the inherited environment,
  which therefore **overrides it**, and that result is what the command is given.
  After applying, `os.Environ()` *is* the merge.
  The file-only set is the part that cannot be recovered - once applied,
  a file-sourced variable is indistinguishable from an inherited one.
  That distinction is issue #39: `-clean` needs the file's set alone as `cmd.Env`,
  the default needs it merged.
  A library returning only the merge cannot serve it, and issue #39 alone justifies the field.
- `Result.Notes` - non-fatal findings.
  A failed close of the file is the only one raised so far,
  and is a Note rather than an error because the file has already given everything it had:
  refusing to run over it would withhold a working environment
  for a problem that no longer affects the command.
  A repeated name is the next, once the parser detects one at all - see *Semantics #3 needs*.
- Rejections stay fatal but become inspectable in two ways because callers want two different things.
  Each problem wraps one of `ErrNotAPair`, `ErrInvalidName` or `ErrNUL`,
  and the file's `ParseError` wraps every problem it collected through `Unwrap() []error`,
  so `errors.Is(err, env.ErrInvalidName)` answers *did it fail for this reason?*
  from a caller that need not know `ParseError` exists.
  Counting and locating them still needs the type:  `errors.As` stops at the first match in the tree,
  so `ParseError` keeps `Path` and `[]Problem{Line, Err, Name}`, reached with `errors.AsType`.

##### Rejected: returning `errors.Join(problems...)` alone.

It is the obvious way to wrap several errors, and `Unwrap() []error` is exactly what it produces, so
the reach it buys is kept above. What it cannot carry is the rest: `Path` has
nowhere to live, counting the problems means walking the multi-error interface
by hand, and `errors.Join` renders its errors one per line - where these leave
through a= single `envrun failed:` line, which would put every problem after the
first outside the attribution contract the README documents.

#### Applying or only reporting

A first draft had the library apply nothing and hand everything back,
on the grounds that a pure function is testable and leaves the caller in control.

That is the wrong trade: it pushes a merge-and-apply loop into every importer,
for a package whose entire purpose is to spare them exactly that.
An API that returns work to its caller is no longer simplifying anything.

Purity is also somewhat illusory when the intended outcome *is* a global side effect.
The principle that a package should be handed what it needs rather than reach for
ambient state is a rule about *consumers* of that state,
not about the one package whose job is to populate it.

**So the library offers an applying entry point.**

An importer wants the variables in its own process, with a report attached;
that is the whole reason to import it rather than shell out.

The CLI needs the opposite, and this is a requirement of envrun's own shape,
rather than a convention borrowed from elsewhere:
it must *not* put the variables into its own process,
because what it needs is the set to hand to the command.

Under ADR-001 that set becomes the child's environment across `execve` on *nix.
On Windows there is no fork/exec pair to hand over with,
so envrun stays the parent and gives the same set to `CreateProcess` as `cmd.Env`.
The routes differ; the requirement does not.
In both cases it is the *child's* environment and never envrun's own,
which is why the command's entry point must apply nothing.

Two entry points, then, because envrun genuinely has two callers:

- `env.L=oad(paths ...string) (Result, error)` - resolve and read, apply nothing.
  The CLI's entry point.
- `env.Apply(paths ...string) (Result, error)` - `Load`, then merge, mutating the environment.
  One line for an importer, and still enough returned for issues #3 and #39.

`Apply` mutates process state, so it belongs at the top of `main`, or in
`TestMain`, before anything concurrent starts.
The Go side is safe on its own - `syscall.Setenv` and `Getenv` share an`envLock` mutex -
so the hazard is not Go against Go. It is twofold:

- **cgo.** With cgo linked in, setting a variable also calls C `setenv` through
  the runtime, and `envLock` does not reach C. A C library calling `getenv` on
  another thread can race with the allocator moving `environ` underneath it.
- **Readers that already read.** Anything that captured a value earlier, at
  package init or otherwise, never sees the change. The process environment has
  no notification.

**There is no concurrency-safe alternative**, because mutating the process
environment is inherently a process-global act; no API can make it otherwise.
The way out is not to need it: `Load` returns the same `Result` without touching
the process, leaving the caller to pass the values where they are wanted. That
is a second reason for `Load` to exist, independent of the command's needs.

For a caller following the convention envrun itself uses, the constraint costs nothing.
`main` is the locus of global access - here it sets the log flags and does nothing else -
and calls `realMain` with everything injected, environment included.

Such a caller already has a place for `Apply` before it has anything to race with,
because the layer that populates the global state is by construction the layer that runs before the rest.
The concurrency rule and the injection convention point at the same line,
which is why `Apply` is safe to offer at all, rather than merely safe to document.

#### Rejected: an `autoload` package

The blank import was the shape this ADR set out to deliver:

```go
import _ "github.com/fgm/envrun/autoload" // nothing more
```

It is declined **as currently imagined**. The idea may return in another shape;
this one does not survive its own arguments.

- **Blank or useful, never both.** The moment a program reads `autoload.Err()`,
  the import stops being blank -
  and at that point `env.Apply()` is shorter and carries no package-level state.
  The one-line form exists only for a program that checks nothing.
- **Checking nothing contradicts the parser.** envrun fails a whole file over one bad line,
  on the grounds that a line in the file would have reached nothing without envrun,
  so running without the operator's configuration is worse than not running at all.
  An unchecked autoload does precisely that, silently.
- **Panicking does not rescue it.** The obvious answer - panic in `init`,
  so failure is loud without anyone checking - borrows a precedent that does not transfer.
  `sql.Register`, `regexp.MustCompile` and `template.Must` panic on *programmer* error:
  values the program supplied as constants.
  A malformed `.env` is *input*, and the convention for input is `regexp.Compile`,
  which returns an error.
  A panicking autoload would abort a program in production over a stray file the operator may not know is read,
  inside an `init` no importer can guard.
- **Its one structural advantage does not work.**
  Running before other packages' `init` functions is the only thing `env.Apply` in `main` cannot do -
  but relative init order follows the import graph,
  which the importer does not control, so the advantage is unreliable rather than real.
- **Its search rule is the one silent break in this ADR.**
  Working directory versus an upward walk, as `direnv` and `git` do,
  is a change no compiler catches: the program still builds and reads a different file.
  Everything else here breaks loudly if revised,
  which is why this is the question decided by not shipping rather than by choosing.

The habitat the idea is strongest in - a blank import in a `_test.go`,
one line per package instead of a `TestMain` - is already served better from both sides.
`envrun go test ./...` applies the same file with the same semantics,
from a directory the operator chose, failing before any test runs;
and `env.Apply` in a three-line `TestMain` keeps a real error path for a single package.
Note that `go test` runs each package in its own directory,
so a working-directory autoload would find nothing from a subpackage:
the search rule is at its worst exactly where the package would be most wanted.

`joho/godotenv/autoload` is the expectation being declined here.
That is a compatibility reason to want one, not a design reason,
and it is recorded so that a future proposal answers these five arguments rather than starting fresh.

### Who prints, and how

The library never writes output - it returns what it observed instead -
which leaves the question for the CLI alone.

It reports through two package-level helpers, `fail(err)` and `note(format, args...)`,
writing to `log`'s default logger with the flags cleared.
Two shapes, one destination, no levels.

**That covers diagnostics, not everything envrun writes.**
Output the user *asked for* - `-h`'s usage today, `-version` later -
goes to standard output and carries no prefix,
because it is the command's product rather than a report about it,
and a caller redirecting it wants it apart from the diagnostics.

**The `log` package with its flags cleared, not `slog`.**
That is what is already in place, and what stays:
the prefix lives in the format string, the destination is `log`'s own default,
which is standard error, and the output is plain prefixed lines rather than
structured records.
The decisive argument is about when envrun writes at all, not about convention:

- **On \*nix it writes only before handover.** `syscall.Exec` replaces the process
  image, so no envrun code exists once the command is running,
  and if the exec fails there is no command. There is no concurrency,
  no stream to keep open, nothing to filter, and no second writer to interleave with.
  Every problem `slog` exists to solve is one this path cannot have.
- **Where a parent survives, the one moment envrun writes after the command
  is the moment structure reads worst.** On Windows `run` reports the command's
  own exit status after it has produced all its output, on the same handle -
  Windows has a genuine separate stderr, `STD_ERROR_HANDLE`, which
  `cmd.Stderr = os.Stderr` binds. A bare `envrun: mytool exited with status 1`
  belongs in that stream; `msg="mytool exited with status 1"` does not.
  That is also where the prefix earns its keep: elsewhere a reader could
  attribute a line by position, here only the prefix can.
- **The prefix is an output contract, not a level.** The README documents that a
  line beginning `envrun failed:` means the status is envrun's rather than the
  command's, and `envrun:` reports what the command did.
  A `slog.Handler` choosing between them must key off `Record.Level`,
  which makes a routing fact depend on a severity - so a future non-fatal error
  would have to be logged at `Info` to avoid claiming envrun failed.
- **The formats are reachable, at a price worth naming.** `log` needs no help:
  cleared flags give a bare line and `SetPrefix` gives the other shape.
  `slog` can also produce them, but not through `HandlerOptions`:
  `ReplaceAttr` drops the time and level, leaving `msg="..."`,
  because key=value is what `TextHandler` *is*.
  Reproducing the two shapes takes a custom `slog.Handler` of about twenty lines,
  against four for the helpers it would replace.
- **Levels only become real with issue #3.**
  Until a verbose mode exists there are two message shapes,
  and a level enum of two values is not one.

**The package-level logger is not a defect here.**
It would be one in a program with goroutines; a command that starts none has a
single destination for its whole life, and a global models that.
The one cost is in the tests. They assert on the exact lines envrun prints -
the `envrun failed:` prefix is what makes a status attributable, so it is worth
pinning - which means capturing the output in a buffer.
With a global destination that means `log.SetOutput`:
five sites across three files, and no `t.Parallel()`.
The `testing` package is no help, never touching `log` at all:
`t.Log` is captured per test, but `log.Printf` goes wherever `log` points,
which by default is stderr.

Injecting the destination would remove that, at the price of crossing the
platform split, since `fail` is called from `run` in both `exec_unix.go` and
`exec_other.go` and not only from `realMain`.
Worth doing if the suite ever needs the parallelism, not before.

### Scope: this is not a dotenv library

envrun exists to run a command with an envfile environment as if the command had it internally.
The library path serves that same goal from inside a Go program.

It is **not** an attempt to provide a general `.env` package,
and features are not adopted on the grounds that other `.env` libraries have them.
ADR-001 states the goal as running a command as if it had the environment internally,
rather than adding features around that -
and a feature adopted because a neighbouring project has one is exactly the second thing.

This is why the parser is deliberately narrower than the alternatives,
rejecting multiline quoted values and names outside the accepted character set,
rather than interpreting them.
Variable interpolation inside values - `${OTHER}` - falls on the same side of that line:
it is a feature of the file format, not of running a command with an environment,
and it is declined here for that reason rather than for lack of precedent.
See PR #6 for the request, and ADR-001 for the goal statement.

Stated positively, against the two projects envrun is most likely to be compared with:

| Project          | form                      | file format                           | hand-off to the command |
|------------------|---------------------------|---------------------------------------|-------------------------|
| `joho/godotenv`  | library only              | rich: interpolation, quoting          | n/a                     |
| `Luzifer/envrun` | command only              | plain, plus encrypted files           | supervises the child    |
| `fgm/envrun`     | command+library, one core | narrow: rejects what it cannot honour | transparent             |

envrun is not trying to be a better parser than the first or a more capable runner than the second.
It has **fewer features than both, deliberately**.
What neither offers is a single core serving both deliveries,
with a hand-off that leaves nothing between the caller and the command.

The table describes what is built: both ADRs are accepted,
and all three of this project's cells name code that exists.
It was written down while two of them were still intent, on purpose,
because a stated goal is what a proposed feature gets measured against.
Without one, whatever library came up most recently in discussion becomes the default answer -
which is how interpolation was nearly adopted above,
on no stronger ground than that another project had it.

### Discovery, not composition

`paths ...string` reads two ways - files to search, or files to merge -
and they are different features. It has to mean one of them:
two behaviors on one variadic is how an API becomes ambiguous.

Four things decide it, and none needs looking outside this repository:

- **`-f` already exists, and it names one exact file.**
  Under discovery that is an override - the search with a single candidate.
  Under composition it has no coherent meaning:
- it would either be a one-element merge, which is the same as discovery, or a contradiction of it.
- **`Result.Path` only makes sense under discovery.**
  "Which file was used" has one answer when the first match wins,
- and none when several are layered.
  Reporting it is a requirement of #3, so composition would cost the field.
- **Composition needs a precedence rule between files**,
  and envrun's precedence between the *file* and the *inherited environment* is itself unsettled -
  PR #6 asked for it to be reversed and it never was.
  Adding a second, independent ordering question on top of an open one is not a trade worth making now.
- **The two are not symmetric in cost.**
  Composition can be added later without disturbing discovery -
  a separate parameter, or a separate call.
  Discovery cannot be retrofitted onto a parameter that already merges,
  because programs will by then depend on every listed file being read.

Conventional usage agrees:
a config search path in Go - viper's `AddConfigPath` with `ReadInConfig`,
for one - takes the first match, and layering is a distinct call that a caller opts into.

So `paths...` is a search path, first match wins, and `-f` overrides it.
Composition - `.env` then `.env.local` - stays available as a later additive feature,
but must not share this parameter.

### Semantics issue #3 needs, which do not exist yet

- **A repeated name keeps the last value.**
  Deliberate, and the rule `Merge` already follows,
  so the semantics need no deciding here.
  What is missing is only the detection needed to raise a `Note` for one - see #3.
- **The ordering problem was imported, not found.**
  Earlier drafts of this ADR said `Env` needed a stable order,
  because issue #3 would "dump the computed environment", and that a map could not give one.
  *Issue #3 asks for no such thing.
  Its text is two lines: log *the file being loaded*, and *the keys being overridden*.
  - The first is `Result.Path`.
  - The second is a Note carrying the name and the lines involved, which has its own order.
  - Nothing asks for the whole environment in a stable sequence,
    so the pressure on `Env`'s type came from a requirement that was never written down.

`Env` is a `map[string]string`. What remains open is timing alone:
whether the parser change raising a `Note` for a repeated name lands with this ADR or with #3.

The `Note` it will raise wants more than a string.
A duplicate is inherently *two* positions - declared at one line, overridden at
another - and it needs the name besides.
The file is not among them: it is already `Result.Path`,
named once for the whole result rather than repeated on every finding.
So it arrives as a further implementation of `Note`, carrying those fields.
That is a pure addition, and it is recorded rather than built
because nothing consumes it until #3 exists.

`Note` is **`fmt.Stringer`, not `error`**.
Most notes report nothing that failed: a file with a repeated name is valid,
which is why its fixture is `pass-duplicate-name.env`,
so typing every note as an error would have the API contradict the parser.
The notes that _do_ report a failure implement `error` as well -
`CloseError` wraps what `Close` returned, keeping an `*io/fs.PathError` reachable -
so nothing is lost by the narrower contract.

Callers discriminate notes by type, which is why the implementations are exported.
The sentinel idiom stays on the fatal side:
`errors.Is` earns its keep against `ParseError`, a tree a caller cannot otherwise
walk, and buys nothing against a flat slice already in hand.

**One invariant governs all of it: no value ever leaves this package inside a message.**
`Problem` carries the offending *name* and never the value,
because a malformed line may hold a secret,
and a caller printing what it is handed must not be able to print that secret by accident.
The same rule binds the duplicate Note:
it may name the variable and the lines, never either value.

## Decision

- **Two packages**, with the CLI at the root and `env` beside it.
- **The library's value is semantic parity**:
  one file means one thing whether it is read by the command or imported.
  Symmetry with the CLI is not a reason.
- The library returns data and never writes output.
- **Every logger parameter is rejected** - `io.Writer`, `*log.Logger` and `*slog.Logger` -
  because `Result` already carries everything observed,
  and a logger would be a second channel for the same findings.
- **The library offers both entry points; the CLI may use only the non-applying one.**
  - `Load` for the command, which must not put the variables into its own process;
  - `Apply` for importers, which want exactly that.
- **No value leaves the package inside a message.** Names and line numbers only.
- **This is not a general `.env` package**,
  and a feature is not adopted because other `.env` libraries carry it.
  Variable interpolation is declined on that basis - see PR #6.
- `paths...` is discovery, not composition.
- **`Env` is `map[string]string`.**
  The ordering argument that questioned it rested on a dump issue #3 never asks for.
- **No `autoload` package**, as currently imagined.
  See its section for the five arguments and for what would have to be answered to revisit it.
- **The CLI keeps `log`**, flags cleared, through the two helpers it already has,
  emitting plain prefixed lines on standard error rather than structured records.
  `slog` is declined and the package-level logger kept.
  Amended by #46: that governs diagnostics, and requested output such as `-h`'s
  usage goes to standard output instead.
  See *Who prints, and how*.

- **Duplicate detection lands with issue #3**, which already asks for it:
  "the keys being overridden" is exactly this finding, so it needs no issue of its own.
  "Last wins" is settled and `Note` is the type that will carry it,
  so raising one is a pure addition: a further implementation of the interface,
  reaching callers through `Result.Notes` unchanged.
  What this ADR owes it is that the shape decided here not prevent it, and it does not.
- **The precedence reversal stays with PR #6**, behind a flag as its 2022 reply agreed.
  What this ADR owes it is that the choice remain expressible, and it does:
  `Result.Env` is the file's own set rather than the merge,
  so either rule can still be computed, and `Merge` documents the argument as
  the winner, making the two rules the same call with its operands swapped.
  `Vars.Export` hard-codes inherited-wins, so #6 adds a method or an option there
  rather than redefining what `Export` and `Apply` already promise.
  Note the coupling: with inherited-wins, a file setting `PATH` has no effect at all.

## Consequences

- **The demos are `Example` functions**, in `env/example_test.go`.
  `go test ./...` compiles *and runs* them on every CI run, asserting their
  `// Output:` blocks, so a demo that no longer matches the API fails the build
  rather than going stale; `go vet` separately ties each `ExampleXxx` to a real
  exported `Xxx`. They render on pkg.go.dev under the symbol they name.
  Each writes the file it reads, since only `go test` runs a binary with the
  package directory as its working directory.
- **Issue #3 becomes CLI-only work.** Both its jobs are presentation over `Result`:
  the file it loaded is `Result.Path`, and the keys it overrode are Notes.
- **Issue #39 is served by this split rather than by a special case.**
  `-clean` uses `Result.Env` as-is; the default merges it under the inherited environment.
- **Revisiting this means re-reading the API, not re-measuring.**
  Nothing here rests on a measurement: every finding is about shape,
  and every argument is checkable against this repository.
