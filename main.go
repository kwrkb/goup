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

// version is overridden at release time via -ldflags "-X main.version=v0.2.0".
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
		flag.NewFlagSet("update", flag.ExitOnError).Parse(os.Args[2:])
		err = Update(defaultInstallRoot, defaultBaseURL)
	case "install":
		err = runInstall(os.Args[2:])
	case "rollback":
		flag.NewFlagSet("rollback", flag.ExitOnError).Parse(os.Args[2:])
		err = Rollback(defaultInstallRoot)
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
  update     Download, verify, and install the latest stable Go toolchain (requires sudo)
  install    Install a specific Go version, e.g. goup install 1.26.3 (requires sudo)
  rollback   Restore the previous Go toolchain from the last backup (requires sudo)
  list       List available Go releases (use --all to include beta/rc)
  version    Print goup version and build platform
  help       Show this message`)
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
		fmt.Println("Update available. Run `sudo goup update` to install it.")
	}

	return nil
}

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	pre := fs.Bool("pre", false, "allow installing a beta/rc pre-release")
	fs.Parse(args)

	if fs.NArg() != 1 {
		return fmt.Errorf("install requires exactly one version argument (e.g. `goup install 1.26.3`)")
	}
	return Install(defaultInstallRoot, defaultBaseURL, fs.Arg(0), *pre)
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
	// surface it so `goup list` never hides "what am I running".
	if current != "" && !currentInList {
		fmt.Fprintln(w, "  ...")
		for _, r := range releases {
			if r.Version == current {
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
