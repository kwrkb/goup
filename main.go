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
		fmt.Fprint(os.Stderr, topHelp)
		os.Exit(1)
	}

	cmd, rest := os.Args[1], os.Args[2:]

	// Route the top-level help / version aliases first so they never fall
	// through into a subcommand dispatch that would try to parse them as
	// arguments.
	switch cmd {
	case "help", "-h", "--help":
		if len(rest) > 0 {
			if printHelpFor(os.Stdout, rest[0]) {
				return
			}
			fmt.Fprintf(os.Stderr, "goup: unknown command %q\n\n", rest[0])
			fmt.Fprint(os.Stderr, topHelp)
			os.Exit(1)
		}
		fmt.Print(topHelp)
		return
	case "-v", "--version":
		fmt.Printf("goup %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return
	}

	// Intercept per-subcommand --help / -h before dispatching to the
	// real parser. Without this, the install loop-parser surfaces
	// '--help' as 'flag: help requested' (an error), and the other
	// commands print Go's default 'Usage of <name>:' stub instead of
	// their real help text.
	if _, known := commandHelp[cmd]; known && hasHelpFlag(rest) {
		printHelpFor(os.Stdout, cmd)
		return
	}

	var err error
	switch cmd {
	case "check":
		flag.NewFlagSet("check", flag.ExitOnError).Parse(rest)
		err = runCheck()
	case "update":
		err = runUpdate(rest)
	case "install":
		err = runInstall(rest)
	case "rollback":
		err = runRollback(rest)
	case "list":
		err = runList(rest)
	case "version":
		fmt.Printf("goup %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return
	default:
		fmt.Fprintf(os.Stderr, "goup: unknown command %q\n\n", cmd)
		fmt.Fprint(os.Stderr, topHelp)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "goup: %v\n", err)
		os.Exit(1)
	}
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

	// Read-only pre-flight: if we are already at the latest stable
	// release, do nothing and skip sudo elevation entirely. Pre-v0.3.0
	// `goup update` was a no-op without sudo in this common case, and
	// prompting for a password on every run to say "already up to date"
	// is a regression the CLI layer must absorb because Update() itself
	// only reaches its no-op branch after maybeElevate has already run.
	//
	// Any error from the pre-flight (network down, missing VERSION file,
	// no archive for GOOS/GOARCH) falls through to the normal elevate +
	// Update path so the real command surfaces the underlying error
	// instead of a silent early return.
	if isAlreadyLatest(defaultInstallRoot, defaultBaseURL) {
		current, _ := CurrentVersion(defaultInstallRoot)
		fmt.Printf("Already up to date: %s\n", current)
		return nil
	}

	if err := maybeElevate(defaultInstallRoot, noSudo); err != nil {
		return err
	}
	return Update(defaultInstallRoot, defaultBaseURL)
}

// isAlreadyLatest is the CLI-layer peek used by runUpdate to skip
// elevation when no write would occur. Returns false on any error so
// the caller falls through to the normal write path where the error
// gets surfaced properly.
func isAlreadyLatest(installRoot, baseURL string) bool {
	current, err := CurrentVersion(installRoot)
	if err != nil {
		return false
	}
	releases, err := FetchReleases(baseURL)
	if err != nil {
		return false
	}
	_, file, err := LatestArchive(releases, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false
	}
	return current == file.Version
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
