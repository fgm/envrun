# Installing envrun

The route decides whether the transparency `envrun` offers is felt at all:
`go run` and `go tool` put a `go` process between the caller and the command,
where a built binary does not.
See [Exit status and platform scope](exit-status.md) for what that costs.

## The recommended route

Pin the version in your project, then build the binary from that pin:

```console
$ go get -tool github.com/fgm/envrun@latest
$ go build -o bin/ github.com/fgm/envrun
```

Then run `bin/envrun <myprogram>`.
As a make target the pin can never drift:

```make
bin/envrun: go.mod
	go build -o bin/ github.com/fgm/envrun
```

Bumping the pin rebuilds the binary on the next invocation.
Ignore **the binary**, `/bin/envrun`, rather than `bin/`,
which is often a tracked source directory.
`go build -o <dir>/` creates the directory, so nothing else is needed.

## Why not `go tool` or `go install`

Both work, and both cost something the recipe above does not:

- **`go tool github.com/fgm/envrun`** re-resolves `go.mod` on every run,
  so the version can never drift —
  but the `go` process stays between the caller and the command.
  It forwards four signals (`HUP`, `INT`, `QUIT`, `TERM`),
  and reports a command killed by any signal as exit 1.
  Ordinary non-zero statuses pass through untouched,
  so this is enough where only exit-status fidelity matters.
- **`go install github.com/fgm/envrun@latest`** gives a wrapper-free binary,
  but it pins at install time and nothing re-checks it afterwards,
  and `$GOBIN` is shared:
  one project's install changes which `envrun` every other project gets.
  Running it from inside a consuming module without `@latest`
  installs that module's pinned version, which fixes the pin but not the sharing.
- **`go run`** interposes and does not forward `SIGTERM` at all.

## Dependencies

`envrun` has none at build or run time: it uses only the standard library.
The authoritative check is the build information embedded in the binary,
which lists one `dep` line per linked module:

```console
$ go version -m bin/envrun
bin/envrun: go1.26.7
	path	github.com/fgm/envrun
	mod	github.com/fgm/envrun	v0.0.0-...
	build	...
```

No `dep` lines means no dependencies.

The modules in `go.mod` belong to `staticcheck`, required by the `tool`
directive for linting. They are not linked into the command, but GitHub's
dependency graph reads `go.mod` rather than the build graph, so its SBOM
reports them anyway.
