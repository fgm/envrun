# ADR-001. How envrun hands control to the command it runs

- Status: Accepted
- Date: 2026-08-23
- Ticket: #35
- Scope: how the child process is started, and how its fate is reported, plus
  the installation advice that decides whether the change is felt at all.
  The package split (ADR-002, tracked separately) and the release mechanics
  (#41) are separate decisions and are not settled here.

## Context

envrun reads a `.env` file and runs a command with that environment.
Starting the child with `os/exec` and staying alive as its parent
makes it a supervisor, whether or not it wants to be one.

#35 reports the consequence:
a signal aimed at envrun kills the wrapper
and leaves the child running, reparented to init.

```console
$ envrun -f /dev/null -- sh -c 'sleep 45' &
$ kill -TERM <envrun pid>
```

Terminal use hides this,
because Ctrl-C signals the whole foreground process group
and so reaches the child directly anyway.
The gap shows when a signal is directed at the envrun process alone —
a supervisor, a `just` target, or a plain `kill <pid>`.

The goal stated while preparing the fix reframes what a fix should achieve:

> enable commands to run with an environment from an envfile
> **as if it had it internally**

rather than "add features around that". That distinction is what this ADR
turns on: a wrapper that forwards signals is still a wrapper, and every
signal it does not forward is a defect waiting to be filed.

## Options considered

Measurements were taken on 2026-08-22 on darwin/arm64 unless stated otherwise.
They are recorded here because they are expensive to reproduce
and easy to misremember,
not because they all favour one option.

### 1. Supervise, and forward signals — the issue as filed

Register `signal.Notify`, relay to the child, and exit `128+signo` when the child dies of a signal.

- **A blanket `signal.Notify(c)` delivers SIGURG** — 21 of them in one busy run.
  Go's runtime uses SIGURG for asynchronous preemption, so "forward everything" forwards runtime noise into the child.
- **`Notify` disables default behaviour at registration**,
  which makes a denylist impossible rather than merely awkward:
  - registering SIGPIPE breaks die-on-broken-pipe
  - registering SIGTSTP breaks Ctrl-Z
  - so the set of signals to handle has to be an allowlist,
    and the allowlist is exactly the thing that will be found incomplete later.
- **Some signals cannot be relayed at all.** 
  - `kill -SEGV` with `Notify` registered still dies through `runtime.throw` — `sigcode=2` (`SI_USER`),
    register dump, exit 2. 
  - SIGSEGV, SIGBUS and SIGFPE therefore cannot be forwarded; 
  - SIGKILL and SIGSTOP cannot be caught in the first place.
- **Forwarding and reporting are orthogonal.** 
  - child killed by SIGKILL, or dying of SIGSEGV, is still reported as 137 or 139 correctly,
    because that comes from `Wait`, rather than from anything envrun does. 
  - Reporting does not need forwarding.
- **Job control cannot be observed.** 
  - `os.Process.Wait` takes no options, so a stopped child never wakes it.
  - Seeing stops needs `syscall.ForkExec` and our own `Wait4`, 
    because `os/exec` owns the reaping and a second waiter races it (`ECHILD`).

**Verdict: cannot be made complete.**
The allowlist is unavoidable,
and three signals cannot be relayed even in principle,
so any version of this ships with a known-incomplete taxonomy.

### 2. Replace the process — `syscall.Exec`

After `execve` there is no wrapper left: signals, exit status, 
stdio and the controlling terminal all belong to the command,
because it *is* the process that was envrun.

- This is what `env`, `nohup`, `chpst` and `direnv exec` do.
- It dissolves rather than solves the forwarding question, along with the allowlist,
  `128+signo`, the re-raise question, `Pdeathsig`/`Setpgid`, job control and `WUNTRACED`.
- **It closes #1 as a side effect**: stdin is the process's own, so there is nothing to wire.
- It also explains the exit-status convention adopted for #36 retroactively:
  125, 126, and 127 are the only statuses an exec-style wrapper *can* return,
  because after `execve` it no longer exists to return anything else.
- **Portability**: `syscall.Exec` compiles on linux, darwin and windows,
  but returns `EWINDOWS` at runtime on Windows.
- **The signal mask survives `execve` clean on darwin and linux alike.**
  Measured 2026-08-23 on linux/arm64 (`golang:1.27`, Go 1.27.0)
  by exec'ing into `sh -c 'grep ... /proc/self/status'`,
  from a runtime first made busy enough for async preemption to be running:

  ```
  SigPnd: 0000000000000000
  SigBlk: 0000000000000000
  SigIgn: 0000000000000000
  SigCgt: 0000000000000400
  ```

  - `SigBlk` and `SigIgn` are empty, so nothing is handed to the command blocked or ignored — 
    and that held across *two* chained `execve` calls,
    because `sh -c` with a single command execs straight into `grep`.
  - The `SigCgt` bit is signal 11, SIGSEGV, installed by `grep` itself after exec: 
    caught handlers do not survive `execve`, so it cannot have been inherited.
    The runner is linux/amd64 rather than arm64, which is not a distinction the signal mask makes.
- **Error mapping is not simplifiable.** 
  - `exec.LookPath` gives `ErrNotFound`;
  - a non-executable path gives `fs.ErrPermission`;
  - a non-binary file with `+x` gives `syscall.ENOEXEC`.
    - `errors.Is(ENOEXEC, fs.ErrPermission)` is **false**,
      so ENOEXEC reaches 126 only through its own explicit case.
- **Coverage across `execve` does not work, and is not needed.**
  Measured 2026-08-23 under `go test -covermode=atomic -coverprofile`:
  `runtime/coverage.WriteMetaDir` returns
  "no meta-data available (binary not built with -cover?)",
  and `WriteCountersDir` reports "invoked for program built with -covermode=<invalid>" —
  in the helper process and the test process alike.
  Those APIs work only in a binary built with `go build -cover`.
  `make cover` passes `-covermode=atomic`,
  which is a different thing and does not enable them.
  It is also unnecessary:
  the in-process ENOEXEC test *executes* the `syscall.Exec` statement,
  since exec returns on failure,
  and statement coverage cannot tell the success half of a statement from the failure half.
  A subprocess test is still wanted for **behaviour** —
  that a child's exit 42 passes through —
  but it never needed to reach the coverage profile.

**Verdict: chosen, on \*nix.** See *Decision*.

### 3. Fork and reap by hand — `syscall.ForkExec` with our own `Wait4`

The only option that can observe job-control stops,
and the only one that gives full control over process groups and death signals.

It is also the largest: it takes over responsibilities `os/exec` currently handles correctly,
for a capability nothing has asked for.
Recorded here so that it is visibly declined rather than overlooked —
it is a reasonable design for a process supervisor, but envrun is not one at this stage in its life.

**Verdict: declined at this stage in the project's lifetime, not missed.**

### 4. Do nothing

Defensible only while the reported failure is confined to terminal use,
which it is not: #35 was found in a `just` target whose `trap ... EXIT INT TERM`
could never fire.

**Verdict: not tenable.**

## Findings that apply to every option

Measured facts that bear on every option above, and on the decision below.

### The `go` toolchain interposes

This is what the installation advice has to answer to:
the transparency envrun offers is only as good as the layer it is invoked through.

- **`go run` does not forward SIGTERM.**
  Reproduced live: the child is orphaned to PID 1 — the very failure #35 reports.
- **`go tool` forwards only `{HUP, INT, QUIT, TERM}`** (`cmd/go/internal/tool/signal.go:14`)
  and **remaps a tool's signal death to exit 1** (`cmd/go/internal/tool/tool.go:429-433` in Go 1.27.0).

So under `go tool envrun`, exit-status transparency is lost at the `go` layer,
no matter how faithful envrun is, and USR1, USR2 and WINCH still orphan.
This is not an argument against exec — supervision through the same layer is strictly worse.

**It is an argument about installation, and one route already avoids it.**
`go install github.com/fgm/envrun@latest` produces a binary in `$GOBIN`
that is invoked directly, with no `go` process in between,
so exec's transparency is fully realized there today.
Only `go run` and `go tool` interpose.

Recommending `go install` over `go tool` on that basis would be too quick.
**Both routes carry a real cost,
and the cost attaches to the invocation, not to the `tool` directive.**
From inside a consuming module,
`go install github.com/fgm/envrun` with no `@latest`
installs the version that module's `go.mod` already pins,
so a consumer can hold the pin *and* get a wrapper-free binary.

- **`go tool envrun`** re-resolves the pin on every run, 
  so the version can never drift from `go.mod` — but the `go` process interposes,
  forwarding four signals and flattening signal death to exit 1.
- **A `go install`ed binary** is fully transparent — but it pins at install time,
  nothing re-checks it afterward, and `GOBIN` is shared,
  so one project's install silently changes which envrun every other project gets.
- **A project-local build** — `go build -o bin/ github.com/fgm/envrun` 
  from inside the consuming module — is transparent *and* fixes the drift instead of relocating it.
  It resolves through the same `go.mod` pin, it does not touch `GOBIN`,
  so projects pinning different versions no longer fight,
  and because the output is in the project it can be a build target with `go.mod` as its prerequisite:

  ```make
  bin/envrun: go.mod
  	go build -o bin/ github.com/fgm/envrun
  ```

  Bumping the pin then rebuilds the binary on the next invocation,
  which makes drift structurally impossible rather than merely documented.
  Verified 2026-08-23: `go build -o <dir>/` creates the directory if absent,
  so no `.gitkeep` is needed;
  and building a tool dependency into a directory from a consuming module works as expected.
  **Ignore the built binary, not the directory** — 
  `bin/` is frequently a tracked source directory rather than a scratch one
  (one consuming project keeps six shell scripts and a `//go:build ignore` helper there),
  so `/bin/envrun` in `.gitignore`, never `bin/`.

  **This is the route the README now recommends**,
  with `go tool` as the alternative for anyone who needs only exit-status fidelity.

That drift is not hypothetical. Measured by a consuming project on 2026-08-23:
its `GOBIN` binary was post-#36 and its module pin pre-#36,
so the same `envrun -f /dev/null -- sh -c 'exit 42'` returned 42 from one and 0 from the other.
A README that says "prefer `go install`" without "and reinstall when you bump the pin" trades one silent failure for another.

Note also that `go tool` remaps only `ExitCode() == -1`, that is death by signal;
an ordinary non-zero exit passes through untouched. So a consumer needing
exit-status fidelity for ordinary failures — not signal transparency — is served
by `go tool` plus a current pin, and need not change invocation at all.

#41 is therefore a matter of reach rather than of feasibility: signed release
artifacts extend the wrapper-free route to people without a Go toolchain. It
does not gate this decision.

### Windows has no fork/exec pair

`syscall.Exec` returns `EWINDOWS` there, and that is the OS model rather than a gap in Go:
Windows has no fork/exec pair at all.

Process creation goes through `CreateProcess`,
which always produces a *new* process with a new PID,
so "replace yourself with the command" cannot be expressed.

Its signal and exit-status model differ too — console control events rather than POSIX signals,
and plain exit codes with no signal encoding — so `128+signo` has no meaning there either.

### Classifying a failure to start

**Classification needs three explicit cases, not one.**
`exec.LookPath` reports `EISDIR` for a directory,
and `errors.Is(EISDIR, fs.ErrPermission)` is false,
as is `errors.Is(ENOEXEC, fs.ErrPermission)`.
Without their own arms both fall to the default and report 125, where `bash` reports 126.
The suite pins this against `bash` itself, agreement and divergence alike:
bash retries a shebang-less script under `/bin/sh` and succeeds,
where execve reports ENOEXEC and envrun stops at 126.

## Decision

**Option 2: `syscall.Exec` on \*nix.**
envrun becomes the command rather than supervising it,
so the transparency it offers is the process's own rather than something it emulates.

**Windows keeps `os/exec`**, the shape not being expressible there —
see *Windows has no fork/exec pair*.
Supervision is not a fallback on that platform, it is the only available shape,
and it will not become available later.

**envrun stays buildable and usable everywhere it built before.**
Only `run`, the one step that starts the command, has two implementations,
selected by build tag; the program itself carries no constraint.
Tagging the program instead, with a stub `main` refusing to run off \*nix,
would turn a working platform into a hard failure
in order to improve a different one —
envrun builds and works on windows/amd64 today, `os/exec` being portable.

**This release is therefore \*nix-only in what it guarantees**,
while still building and running commands on Windows through the supervising path.
The distinction is stated in `docs/exit-status.md` under *Platform scope*:
the platform is supported, the transparency is not.

## Consequences

### For the code

- **Two files define `run`, and nothing else is tagged.**
  - `exec_unix.go` and `exec_other.go` each carry an explicit `//go:build` line,
    since neither `_unix` nor `_other` is a GOOS
    and so neither filename constrains anything.
    Naming the second file for the constraint rather than for Windows
    keeps every platform covered by one of the two:
    `!unix` also catches js/wasm and wasip1,
    which compiled before this change and would otherwise stop.
  - `exec_other.go` keeps supervising,
    with `cmd.Stdin/Stdout/Stderr` assigned directly
    instead of the pipe-and-goroutine wiring,
    which closes #1 there and removes the `Wait`-versus-copy race.
  - The exit-status mapping cannot simply lose its `*exec.ExitError` branch:
    `realMain` is shared while only `run()` is build-tagged,
    so Windows still produces one,
    and dropping the branch would reintroduce #36 there.
    It lives **in `exec_other.go`**,
    the only file where a command's own status is envrun's to report.
    `main.go` keeps `exitStatus`,
    the classification of envrun's *own* failures, which both platforms share.
  - `tests.yml` runs a Windows job, so a regression in any of it fails visibly.
- **Testability.** Exec never returns on success,
  so the tests split in two, and the split follows the statuses.
  - The pre-exec failures — 125, 126, 127 — stay **in process**,
    and are the ones #18 lists as uncovered.
    ENOEXEC reaches the `syscall.Exec` call in process;
    so do EINVAL (a NUL in a value, demonstrated) and, on linux,
    ETXTBSY and E2BIG — ENOEXEC is not the only one.
  - Everything past the point where the command starts needs a **subprocess**,
    and the test binary is its own: it re-enters as envrun,
    or as a helper reporting one fact about the process it runs in.
  - The sharp assertion for #35 is **process identity**, not the issue's own repro.
    Killing envrun and watching for an orphan races on whether the orphan is still there;
    comparing the PID the command reports against the PID the caller started
    does not, and no wrapper can pass it whatever it forwards.
  - The subprocess half contributes nothing to the coverage profile, and cannot:
    the flush APIs need a `go build -cover` binary, as measured above.
    It is there for behaviour, not for the percentage.
- **`exitStatus` needs three explicit arms** — `ErrNotFound`, `fs.ErrPermission`,
  and `EISDIR` and `ENOEXEC` each on its own,
  for the reason measured under *Classifying a failure to start*.

### For the project

- **envrun stops being observable once the command starts.** 
  - No wrapper means no place to log, count, time, or retry.
  - Anything of that kind must happen before `execve` or not at all —
    which also means #3's verbose flag can only ever report on the environment,
    never on the command's behaviour.
- **The README gets shorter, not longer.** The reasoning moves here, 
  so a reader evaluating the package does not meet a signal taxonomy first.
  The `go tool` caveat above must appear there, or #35 gets reopened.
- **Supervision on Windows can still be improved, and option 1 is the way to do it there.**
  The objection that kills the allowlist on Unix does not transfer:
  it fails there because the vocabulary is thirty-odd signals
  of which three cannot be relayed at all,
  whereas on Windows it is essentially Ctrl+C and Ctrl+Break,
  so an allowlist is complete almost by construction.
  Worth investigating alongside it: job objects with kill-on-close,
  which are the Windows analogue of `Pdeathsig`
  and would address the orphaning in #35 directly.
  Out of scope for #35: **#44**.
- **Revisiting this means measuring first**:
  - the linux signal mask across `execve`,
  - and whether the installation route in the README still routes through the `go` command.

## Provenance

Adversarial review on 2026-08-22 and 2026-08-23 supplied several of the measurements above,
by running them rather than reasoning about them.
Every claim here that reads as settled was executed;
a claim recorded as settled without being run
is what this ADR is the least able to catch on its own.
