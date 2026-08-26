// Envrun runs a command with default environment variables taken from a file,
// then gets out of the way.
//
// Usage:
//
//	envrun [-f file] command [argument ...]
//
// The flags are:
//
//	-f file
//		Read the environment from file
//		instead of .env in the working directory.
//
// The file is read, never sourced — no expansion and no execution — so a value
// reaches the command exactly as written. Its variables are merged under the
// inherited environment, so a name already exported wins over the file.
//
// On *nix envrun does not supervise the command: it becomes it, through execve,
// keeping the same process. What follows are therefore the command's own:
//
//   - the signals it receives,
//   - the exit status it reports,
//   - its standard streams,
//   - the controlling terminal.
//
// Windows has no equivalent and keeps a supervising parent.
// See docs/adr/001-handing-control-to-the-command.md.
//
// Statuses 125, 126 and 127 report envrun's own failures before the command
// starts, following the convention of coreutils env, timeout and nohup.
// A command exiting 125 itself stays distinguishable,
// because envrun names itself on standard error whenever one of these is its own.
// See docs/exit-status.md.
//
// The file format, and the reading a Go program can import to obtain the same
// environment in its own process with no wrapper at all, are in
// [github.com/fgm/envrun/env].
package main
