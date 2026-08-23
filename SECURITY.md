# Security Policy

## Supported Versions

Fixes go into the latest tagged release only.
Until 1.0 that means the newest `v0.x` tag:
there are no maintenance branches, so an earlier tag is superseded, never patched.

| Version           | Supported          |
|-------------------|:------------------:|
| latest `v0.x` tag | :white_check_mark: |
| earlier tags      | :x:                |
| `main` at HEAD    | :x:                |


## Reporting a Vulnerability

To report a vulnerability:

- please do NOT use the issue system on this repo
- use the contact form on https://osinet.fr/contact
- the first response on that form should be within 1 day Monday to Friday

If a vulnerability is:
- accepted: we can work together on a fix, and you will be credited (unless you prefer not to be) on the fix
- considered not to be an actual security issue: you will get a suggestion to open it,
  as an issue on the github issue system for the repo
- rejected: as you prefer, it can be either reported as an issue,
  and closed with an explanation about why it is not an actual issue; or remain private.
  The former is usually better.

## Security properties, and their limits

### The file is read, never sourced

envrun invokes no shell — not to read the environment file,
and not to start the command.
The file is parsed line by line,
and the command is started through `execve` with an argument array rather than a command string.
There is no point at which the file's contents are interpreted as code.

A shell tasked with loading the same file with `set -a; . .env` would expand `$VAR` and `${VAR}`,
run `$(...)` and backticks, glob `*`, and expand `~`.
envrun does none of these: a value is transported exactly as written.

```
SUBST=$(touch /tmp/pwned)     ->  the value is that text; nothing runs
BACKTICK=`touch /tmp/pwned`   ->  the value is that text; nothing runs
GLOB=*                        ->  the value is one asterisk; no filenames
TILDE=~                       ->  the value is one tilde; no home directory
```

This is what makes a file from a semi-trusted source — a checkout, a CI
artifact, a pasted snippet — safe to load:
its contents can become the value of a variable, never a command envrun runs.

**Where this one stops.**

envrun is not a sandbox around the command you asked for,
which runs with whatever it was given.
The property is only that the *file* cannot add to it.

The corollary is a correctness matter rather than a security one.
Quotes are part of the value, so `FOO="bar"` sets `"bar"` with its quotes,
where a shell would strip them.
A file read by both must keep its values plain.
See [The environment file](docs/environment-file.md).

### Secrets in diagnostics

An environment file often holds secrets,
and envrun writes its diagnostics to standard error —
which under CI is a log that is retained, searched, and often world-readable.

So envrun reports **names and line numbers, never the text after the first `=`**.
That guarantee holds at every point where the library builds a message:

- a rejected name is `line 5: invalid name "9LEADING"` — the name is quoted only
  where the name is itself the fault;
- a NUL in a value is `line 3: value contains NUL`,
  naming neither the value nor the variable that held it;
- a line too long to hold is `line 2: too long`, naming the line and never its contents;
- a variable that cannot be set is its name and the system's
  `setenv: invalid argument`.

Values do reach the command envrun runs — that is what envrun is for.
This is a property of the diagnostics alone.

**Where this one stops.**

envrun splits a line at its **first `=`**,
so on a malformed line the "name" is everything before that `=`.
A line that is *only* a pasted secret, and whose own bytes contain an `=`,
is therefore reported as a bad name, with the secret inside it:

```
https://user:pw@h/p?k=v   →   line 1: invalid name "https://user:pw@h/p?k"
aGVsbG8+d29ybGQ=          →   line 1: invalid name "aGVsbG8+d29ybGQ"
```

Both halves are needed for this: a stray line, and an `=` within it.
A stray line with no `=` at all is reported as `line 1: not a name=value pair`,
quoting nothing.

If it does happen, treat that value as disclosed wherever the log is kept, and rotate it. 
Nothing else in the file is affected, every other line being reported by number.
