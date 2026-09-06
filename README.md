# envrun

[![GoDoc](https://pkg.go.dev/badge/github.com/fgm/envrun)](https://pkg.go.dev/github.com/fgm/envrun)
[![CI](https://github.com/fgm/envrun/actions/workflows/tests.yml/badge.svg)](https://github.com/fgm/envrun/actions/workflows/tests.yml)
[![codecov](https://codecov.io/gh/fgm/envrun/branch/main/graph/badge.svg?token=8YYX1B720M)](https://codecov.io/gh/fgm/envrun)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/fgm/envrun/badge)](https://scorecard.dev/viewer/?uri=github.com/fgm/envrun)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/14232/badge)](https://www.bestpractices.dev/projects/14232)

The `envrun` command runs any command with default environment variables taken from a file,
then gets out of the way.

Variables already present in the environment override the ones in the file.
The command may have arguments, and it will be looked up in the `$PATH` if its name does not contain a `/`.

Many programs, and most IDEs in their run configurations, can read a `.env` file themselves.
`envrun` is for everywhere else — CI/CD, a `just` or `make` target, a plain shell —
where the program to be run cannot.

If the program is yours and written in Go, you need no wrapper at all:
import [`github.com/fgm/envrun/env`](https://pkg.go.dev/github.com/fgm/envrun/env)
and call `env.Apply()` at the top of `main`.
It reads the same file by the same rules,
so a project can use the command in CI and the library in its own binaries
without keeping two dialects of one format in its head.

On \*nix, `envrun` does not wrap the command:
it **becomes** it, through `execve`, keeping the same process.
Signals, exit status, standard streams and the controlling terminal
are the command's own, exactly as if it had read the environment file itself.
Windows has no equivalent and keeps a supervising parent —
see [Exit status and platform scope](docs/exit-status.md).

## Installing

**Without a Go toolchain**, download the archive for your platform from the
[latest release](https://github.com/fgm/envrun/releases/latest), unpack it,
and put `envrun` on your `$PATH`.
Every release is built by GitHub Actions and carries a Sigstore attestation
of the repository, workflow and commit it came from.

**With one**, pin the version in your project and build the binary from that pin,
so the version follows the project rather than the machine:

```console
$ go get -tool github.com/fgm/envrun@latest
$ go build -o bin/ github.com/fgm/envrun
```

Then run `bin/envrun <myprogram>`.
`go tool` and `go install` work too, each at a cost in transparency or in pinning.
Verifying a download, and the full comparison, is in the
[Installing](docs/installing.md) document.

## Running

- `envrun foo`: run `foo` with the environment defaults loaded from `.env`,
  or fail if it is missing or cannot be read.
- `envrun -f other.env env`: run the `env` command with the defaults loaded from `other.env`.

The file is required either way.
A run with no defaults to add is a run that does not need `envrun`,
so a missing file is a failure rather than a silent pass-through:
configuration that was meant to be there is worth stopping for.

`envrun` reads the file, it never sources it — no expansion, no execution,
so `DSN=postgres://${INSTANCE}/db` reaches the command exactly as written.
A shell sourcing the same file would substitute instead,
so keep values plain if both read it.
The format, and what it refuses to carry, is in
[The environment file](docs/environment-file.md).

In a clone, `make build` writes the binary to `bin/envrun`.

`make demo` builds a file from the parsing fixtures in `env/testdata/` and runs `env` against it.
Those fixtures are what the test suite asserts against,
so the demo cannot drift from the documented behaviour.

## Exit status

The command reports for itself.
Apart from `-h`, which prints usage on standard output and exits `0`,
`envrun` has statuses only for its own failures, before the command starts,
following the convention of coreutils `env`, `timeout` and `nohup`:

- `125`: `envrun` itself failed — the environment file could not be read,
  a flag was not understood, or no command was given
- `126`: the command exists but could not be executed
- `127`: the command could not be found

A command exiting `125` itself is indistinguishable by status alone,
so standard error carries the answer:
every failure of `envrun`'s own is prefixed `envrun failed:`.
A `0` from `-h` carries no such line, having failed at nothing.
The rest, and the Windows divergence, is in
[Exit status and platform scope](docs/exit-status.md).

## Dependencies

None at build or run time: `envrun` uses only the standard library.
The modules in `go.mod` belong to `staticcheck`, used for linting.
Each release archive ships an SPDX SBOM built from the binary itself,
so the claim is checkable without a toolchain —
see the [Installing](docs/installing.md#dependencies) document.

## Related tools

`envrun` optimises for being invisible.
The command runs exactly as if it had read the file itself —
same process, same signals, same exit status, same terminal —
and nothing in the file is ever expanded or executed,
so a value cannot mean one thing to a shell and another to the program.
No dependencies, standard library only,
checkable with `go version -m` or with the SBOM shipped in every release.

- **[godotenv](https://github.com/joho/godotenv)** is a library:
  the program does the loading, so it has to be Go, and it has to be changed.
  `envrun` leaves the program untouched, in any language,
  including ones you do not control.
- **[Luzifer/envrun](https://github.com/Luzifer/envrun)** is a feature-rich take
  on the same name and idea, encrypted environment files included.
  If that is what you need, take it.
  This one is deliberately smaller:
  a format that cannot surprise you, and a process that gets out of the way.

## Design

Why `envrun` replaces itself with the command rather than supervising it,
and the measurements behind that choice:
[ADR-001](docs/adr/001-handing-control-to-the-command.md).

Why the parsing lives in an importable package, what shape that package has,
and what was declined on the way:
[ADR-002](docs/adr/002-splitting-the-command-from-the-library.md).

## Developing

Standard Go tooling works directly: `make test`, `make lint`
(`go tool staticcheck ./...`), and `make build`.
Run `make demo` to watch `envrun` inject `.env.demo` into the `env` command.

A [Dev Container](https://containers.dev) is also provided under
[`.devcontainer/`](.devcontainer/README.md) for a reproducible,
editor-agnostic environment (open it with VS Code, JetBrains/Gateway, or the
`devcontainer` CLI). It is purely additive: native development is unaffected.


## Support

- Non-security questions: use [Github issues](https://github.com/fgm/envrun/issues)
- Security questions or direct support: use https://osinet.fr/contact
