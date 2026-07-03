package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// decision names the three outcomes maybeElevate can pick for a write
// command: run in-process, re-exec through sudo, or fail fast with a
// permission hint.
type decision int

const (
	decisionRun decision = iota
	decisionElevate
	decisionFail
)

// elevationDecision picks run/elevate/fail from the four runtime signals.
// It is pure so the branching is fully unit-testable without touching
// /usr/local, a real TTY, or sudo.
//
// Order matters: uid == 0 short-circuits before canWrite so a root shell
// that somehow lost write access to /usr/local (weird mount, chattr +i)
// still runs the command and surfaces the real failure at write time,
// instead of trying to re-exec sudo from an already-root process.
func elevationDecision(uid int, canWrite, tty, noSudo bool) decision {
	if uid == 0 || canWrite {
		return decisionRun
	}
	if noSudo || !tty {
		return decisionFail
	}
	return decisionElevate
}

// isTTY reports whether goup should attempt an interactive sudo prompt.
// Hybrid check honoring the CLAUDE.md contract that CI, cron, pipes, AND
// stdin redirects all fast-fail: require BOTH a usable controlling
// terminal AND a stdin that itself looks interactive.
//
//  1. /dev/tty must open — this is what sudo actually reads passwords
//     from. Fails under systemd/cron/CI where no controlling terminal
//     exists.
//  2. stdin must be a character device — screens out pipes (`| goup`)
//     and regular-file redirects (`goup < script.sh`) even when a
//     controlling terminal is available.
//
// Known corner: `goup update < /dev/null` slips through because
// /dev/null is itself a character device. Distinguishing /dev/null from
// a real tty on the stdin fd requires an isatty ioctl (see
// golang.org/x/term), which we deliberately refuse for stdlib-only.
// Documented as an accepted trade-off; pass --no-sudo to make the
// non-interactive intent explicit.
func isTTY() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	f.Close()

	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// maybeElevate is the CLI-layer wrapper the write commands call before
// their real work. It returns nil for decisionRun so the caller proceeds,
// re-execs via sudo for decisionElevate (does not return on success), or
// returns the wrapped write-access error for decisionFail so main() prints
// it with the standard "goup: ..." prefix.
func maybeElevate(installRoot string, noSudo bool) error {
	writeErr := checkWritable(installRoot)
	canWrite := writeErr == nil

	switch elevationDecision(os.Getuid(), canWrite, isTTY(), noSudo) {
	case decisionRun:
		return nil
	case decisionFail:
		return writeErr
	case decisionElevate:
		return reexecWithSudo()
	}
	return nil
}

// reexecWithSudo replaces the current process with `sudo <abs-self> <args...>`.
// syscall.Exec (not exec.Command) hands stdio, signals, and the exit code
// straight to sudo — no child-process plumbing.
//
// The self path comes from os.Executable() so sudo's secure_path can't
// lose a goup binary that lives outside /usr/local/bin (~/go/bin, the
// exact case that motivated v0.3.0).
func reexecWithSudo() error {
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found in PATH: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving goup binary path: %w", err)
	}

	argv := append([]string{"sudo", self}, os.Args[1:]...)
	fmt.Fprintln(os.Stderr, "goup: elevating via sudo ...")
	return syscall.Exec(sudoPath, argv, os.Environ())
}
