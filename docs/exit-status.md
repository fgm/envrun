# Exit status and platform scope

What `envrun` reports for its own failures,
how to tell those from the command's own status,
and what Windows does differently.

## Exit status

The command reports for itself.
On \*nix there is no `envrun` left once it starts,
so its status, including death by a signal,
reaches the caller unchanged and untranslated.

Apart from `-h`, which prints usage on standard output and exits `0`
without reading the environment or running anything,
`envrun` has statuses only for its own failures, before the command starts.
They follow the convention used by coreutils `env`, `timeout` and `nohup`,
which keeps them out of the range a command is likely to use:

- `125`: `envrun` itself failed — the environment file could not be read,
  a flag was not understood, or no command was given
- `126`: the command exists but could not be executed
- `127`: the command could not be found

A command which itself exits `125`, `126` or `127`
is indistinguishable from these *by status alone*,
which is unavoidable and is why the range was chosen.
Standard error tells them apart,
because every failure of `envrun`'s own names the process that failed:

```console
envrun failed: reading .env: open .env: no such file or directory
```

Among `125`, `126` and `127` that line is present exactly when the status is
`envrun`'s; where it is absent, the status belongs to the command.
The qualifier matters because `-h` is envrun's own `0` and carries no such line:
it did not fail, and nothing ran.

`envrun` starts what the operating system can start, and no more:
a file with the execute bit but no shebang and no loadable format reports `126`,
where a shell would retry it under `/bin/sh`.
Choosing an interpreter is not `envrun`'s job.

## Platform scope

The transparency above is \*nix-only, and permanently so.
Windows has no fork/exec pair —
`CreateProcess` always makes a new process with a new PID —
so "replace yourself with the command" cannot be expressed there.
Windows is supported and tested:
`envrun` builds and runs commands, standard input included,
with a supervising parent that passes the command's exit status through.
What it cannot offer is the same process.
Improving supervision there is tracked as
[#44](https://github.com/fgm/envrun/issues/44).

Being still there, it also reports what it saw the command do:

```console
envrun: sh exited with status 42
```

Prefixed `envrun:` and never `envrun failed:` —
`envrun` did not fail, and the status is the command's.

Why `execve` rather than forwarding signals from a wrapper is recorded in
[ADR-001](adr/001-handing-control-to-the-command.md),
with the measurements behind it.
