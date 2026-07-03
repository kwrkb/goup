package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

const (
	defaultInstallRoot = "/usr/local"
	defaultBaseURL     = "https://go.dev/dl"
)

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
	case "rollback":
		flag.NewFlagSet("rollback", flag.ExitOnError).Parse(os.Args[2:])
		err = Rollback(defaultInstallRoot)
	case "help", "-h", "--help":
		usage()
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
  rollback   Restore the previous Go toolchain from the last backup (requires sudo)
  help       Show this message`)
}

func runCheck() error {
	current, err := CurrentVersion()
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
