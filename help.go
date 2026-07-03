package main

import (
	"fmt"
	"io"
)

// Help text lives here so the top-level Usage and each `goup help <cmd>`
// stay in sync. Every subcommand help follows the same structure:
//
//     <signature>
//     <one-paragraph what/why>
//     [Arguments:]
//     [Options:]
//     [Example(s):]
//
// so agents (and humans) can predict where to look for each piece.

const topHelp = `goup - Safely swap the Go toolchain installed at /usr/local/go

Usage:
  goup <command> [args] [options]
  goup help [<command>]

Read-only commands:
  check                       Show current and latest stable Go versions (no side effects)
  list [-n <N>] [--all]       List available Go releases (newest first)
  version                     Print goup version and build platform

Write commands (need root on /usr/local; auto re-exec via sudo on a TTY):
  update [--no-sudo]          Download, verify, and install the latest stable Go toolchain
  install <version> [--pre] [--no-sudo]
                              Install a specific Go version (e.g., 1.26.3, go1.26.3)
  rollback [--no-sudo]        Restore the previous Go toolchain from the last backup

Global options:
  -h, --help                  Show this help ('goup help <command>' for per-command detail)
  -v, --version               Print goup version

Elevation: on an interactive terminal the write commands re-invoke themselves
via sudo automatically when /usr/local is not writable. In non-interactive
contexts (CI, cron, pipes, regular-file redirects) or with --no-sudo, they
fast-fail with a permission-denied hint instead. See 'goup help update'.
`

const checkHelp = `goup check

Show the currently installed Go version and the latest stable version
published by go.dev. Reads /usr/local/go/VERSION directly and never
writes to disk, so no sudo is needed.

Example:
  goup check
`

const updateHelp = `goup update [--no-sudo]

Download, verify, and install the latest stable Go toolchain into
/usr/local/go. Sequence: fetch release list -> skip if already latest ->
download tarball to temp dir -> sha256 verify -> back up existing
/usr/local/go to /usr/local/go.bak.<timestamp> -> extract -> run
'go version' on the new install -> auto-rollback on any failure.

Options:
  --no-sudo                   Do not re-exec via sudo when /usr/local is not
                              writable; fail fast with a permission hint
                              instead (for CI, scripts)

Examples:
  goup update                          # interactive: prompts for sudo if needed
  goup update --no-sudo                # CI: fail fast without prompting
`

const installHelp = `goup install <version> [--pre] [--no-sudo]

Pin a specific Go version. Same download -> verify -> backup -> extract ->
launch -> auto-rollback sequence as 'goup update'. If <version> equals
the currently installed version, it is a no-op.

Arguments:
  <version>                   Go version to install (e.g., 1.26.3, go1.26.3;
                              both forms accepted, case-insensitive)

Options:
  --pre                       Allow installing a beta/rc pre-release
                              (e.g., 1.27rc1). Refused without this flag.
  --no-sudo                   Do not re-exec via sudo when /usr/local is not
                              writable; fail fast instead

Examples:
  goup install 1.25.11
  goup install go1.25.11               # same version, alternate spelling
  goup install 1.27rc1 --pre           # opt into a pre-release
`

const rollbackHelp = `goup rollback [--no-sudo]

Restore /usr/local/go from the most recent backup left by 'goup update'
or 'goup install'. Fails if no backup exists. Only one generation of
backup is retained, so rollback works exactly once between writes.

Options:
  --no-sudo                   Do not re-exec via sudo when /usr/local is not
                              writable; fail fast instead

Example:
  goup rollback
`

const listHelp = `goup list [-n <N>] [--all]

List available Go releases newest first. The currently installed version
is marked with '*'. If it falls outside the -n window, it is still shown
after a '...' separator so this command never hides what you are running.

Options:
  -n <N>                      Maximum number of releases to display (default 10)
  --all                       Include beta/rc pre-releases (default: stable only)

Examples:
  goup list                            # 10 most recent stable releases
  goup list --all -n 20                # 20 rows including pre-releases
`

const versionHelp = `goup version

Print the goup release tag, target OS/arch, and the Go version used to
build it. Reports 'dev' for local 'go build' output; release binaries
have the tag baked in via -ldflags.

Example:
  goup version                         # goup v0.3.0 (linux/amd64, go1.26.4)
`

// commandHelp routes 'goup help <cmd>' and 'goup <cmd> --help' to the
// per-command help text. Add new commands here when they gain their
// own help entry.
var commandHelp = map[string]string{
	"check":    checkHelp,
	"update":   updateHelp,
	"install":  installHelp,
	"rollback": rollbackHelp,
	"list":     listHelp,
	"version":  versionHelp,
}

// hasHelpFlag reports whether the argument slice contains a help-request
// token. We intercept these in main() so subcommand help works uniformly
// regardless of the underlying flag-parsing strategy — the install
// loop-parser in particular used to surface 'flag: help requested' as
// an error on '--help'.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--help", "-help":
			return true
		}
	}
	return false
}

// printHelpFor writes the per-command help text for cmd to w. Returns
// false if cmd has no dedicated help entry, so callers can fall back
// to the top-level usage.
func printHelpFor(w io.Writer, cmd string) bool {
	h, ok := commandHelp[cmd]
	if !ok {
		return false
	}
	fmt.Fprint(w, h)
	return true
}
