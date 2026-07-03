package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
)

const (
	defaultInstallRoot = "/usr/local"
	defaultBaseURL     = "https://go.dev/dl"
)

// version is overridden at release time via -ldflags "-X main.version=v0.3.0".
// It stays "dev" for `go build` / `go run` so unreleased binaries are obvious.
var version = "dev"

func main() {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "goup: Windows is not supported; manage the Go toolchain manually.")
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "check":
		flag.NewFlagSet("check", flag.ExitOnError).Parse(os.Args[2:])
		err = runCheck()
	case "update":
		err = runUpdate(os.Args[2:])
	case "install":
		err = runInstall(os.Args[2:])
	case "rollback":
		err = runRollback(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	case "version", "-v", "--version":
		fmt.Printf("goup %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return
	default:
		fmt.Fprintf(os.Stderr, "goup: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "goup: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: goup <command>

Commands:
  check      Show current and latest stable Go versions (no side effects)
  update     Download, verify, and install the latest stable Go toolchain
  install    Install a specific Go version, e.g. goup install 1.26.3
  rollback   Restore the previous Go toolchain from the last backup
  list       List available Go releases (use --all to include beta/rc)
  version    Print goup version and build platform
  help       Show this message

Write commands (update, install, rollback) need root on /usr/local.
On an interactive terminal goup re-execs itself via sudo automatically.
Pass --no-sudo to opt out (CI, scripts) and get the previous fast-fail
behaviour instead.`)
}

func runCheck() error {
	current, err := CurrentVersion(defaultInstallRoot)
	if err != nil {
		return err
	}

	releases, err := FetchReleases(defaultBaseURL)
	if err != nil {
		return err
	}

	_, file, err := LatestArchive(releases, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	fmt.Printf("Current: %s\n", current)
	fmt.Printf("Latest:  %s\n", file.Version)

	if current == file.Version {
		fmt.Println("Up to date.")
	} else {
		fmt.Println("Update available. Run `goup update` to install it.")
	}

	return nil
}

func runUpdate(args []string) error {
	noSudo, err := parseWriteFlags("update", args)
	if err != nil {
		return err
	}
	if err := maybeElevate(defaultInstallRoot, noSudo); err != nil {
		return err
	}
	return Update(defaultInstallRoot, defaultBaseURL)
}

func runRollback(args []string) error {
	noSudo, err := parseWriteFlags("rollback", args)
	if err != nil {
		return err
	}
	if err := maybeElevate(defaultInstallRoot, noSudo); err != nil {
		return err
	}
	return Rollback(defaultInstallRoot)
}

func runInstall(args []string) error {
	version, pre, noSudo, err := parseInstallArgs(args)
	if err != nil {
		return err
	}
	if err := maybeElevate(defaultInstallRoot, noSudo); err != nil {
		return err
	}
	return Install(defaultInstallRoot, defaultBaseURL, version, pre)
}

// parseWriteFlags handles the flags shared by write commands with no
// positional arguments (update, rollback). Currently only --no-sudo.
func parseWriteFlags(name string, args []string) (noSudo bool, err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ns := fs.Bool("no-sudo", false, "do not re-exec via sudo when write access is missing")
	if perr := fs.Parse(args); perr != nil {
		return false, perr
	}
	if fs.NArg() > 0 {
		return false, fmt.Errorf("%s does not accept positional arguments", name)
	}
	return *ns, nil
}

// parseInstallArgs accepts the flags and the version argument in either
// order (`install --pre 1.27rc1` and `install 1.27rc1 --pre` are both
// valid). Go's flag package stops at the first non-flag token, so we
// loop-parse: consume flags, then peel off one positional token, then
// resume flag parsing on the rest.
func parseInstallArgs(args []string) (version string, pre bool, noSudo bool, err error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	// Suppress flag's built-in stderr output; main() already prints errors
	// with a "goup:" prefix, so letting flag print too would double up.
	fs.SetOutput(io.Discard)
	preFlag := fs.Bool("pre", false, "allow installing a beta/rc pre-release")
	noSudoFlag := fs.Bool("no-sudo", false, "do not re-exec via sudo when write access is missing")

	for len(args) > 0 {
		if perr := fs.Parse(args); perr != nil {
			return "", false, false, perr
		}
		if fs.NArg() == 0 {
			break
		}
		if version != "" {
			return "", false, false, fmt.Errorf("install accepts only one version argument")
		}
		version = fs.Arg(0)
		args = fs.Args()[1:]
	}

	if version == "" {
		return "", false, false, fmt.Errorf("install requires a version argument (e.g. `goup install 1.26.3`)")
	}
	return version, *preFlag, *noSudoFlag, nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	all := fs.Bool("all", false, "include beta/rc releases")
	limit := fs.Int("n", 10, "maximum number of releases to display")
	fs.Parse(args)

	releases, err := FetchAllReleases(defaultBaseURL)
	if err != nil {
		return err
	}

	// current is best-effort: goup list should still work when no toolchain
	// is installed yet, so we swallow the read error and just skip the marker.
	current, _ := CurrentVersion(defaultInstallRoot)

	return renderReleaseList(os.Stdout, releases, current, *all, *limit)
}

// renderReleaseList writes a filtered, marked release list to w. Split from
// runList so it can be tested without a live go.dev connection.
func renderReleaseList(w io.Writer, releases []Release, current string, all bool, limit int) error {
	shown, currentInList := 0, false
	for _, r := range releases {
		if !all && !r.Stable {
			continue
		}
		if shown >= limit {
			break
		}
		printRelease(w, r, current == r.Version)
		if current == r.Version {
			currentInList = true
		}
		shown++
	}

	// If the installed toolchain is older than the window just printed,
	// surface it so `goup list` never hides "what am I running". Only emit
	// the "..." separator when we can actually find the current version in
	// the release list; otherwise we would leave a dangling ellipsis with
	// nothing after it (e.g. dev builds or ancient versions dropped from
	// the go.dev API).
	if current != "" && !currentInList {
		for _, r := range releases {
			if r.Version == current {
				fmt.Fprintln(w, "  ...")
				printRelease(w, r, true)
				break
			}
		}
	}

	if shown == 0 {
		return fmt.Errorf("no releases matched")
	}
	return nil
}

func printRelease(w io.Writer, r Release, isCurrent bool) {
	marker := " "
	if isCurrent {
		marker = "*"
	}
	kind := "stable"
	if !r.Stable {
		kind = "pre-release"
	}
	fmt.Fprintf(w, "%s %-12s %s\n", marker, r.Version, kind)
}
