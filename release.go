package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// ReleaseFile describes a single downloadable artifact for a Go release,
// as returned by the go.dev/dl JSON API.
type ReleaseFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Sha256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}

// Release describes a single Go version entry from the go.dev/dl JSON API.
type Release struct {
	Version string        `json:"version"`
	Stable  bool          `json:"stable"`
	Files   []ReleaseFile `json:"files"`
}

// FetchReleases retrieves the Go release list from baseURL (e.g. https://go.dev/dl).
func FetchReleases(baseURL string) ([]Release, error) {
	url := strings.TrimRight(baseURL, "/") + "/?mode=json"

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching release list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching release list: unexpected status %s", resp.Status)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decoding release list: %w", err)
	}

	return releases, nil
}

// LatestArchive returns the newest stable release and its archive (tar.gz)
// file matching goos/goarch. Releases are assumed to be ordered newest first,
// which matches the go.dev/dl JSON API.
func LatestArchive(releases []Release, goos, goarch string) (Release, ReleaseFile, error) {
	for _, r := range releases {
		if !r.Stable {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == goos && f.Arch == goarch {
				return r, f, nil
			}
		}
	}
	return Release{}, ReleaseFile{}, fmt.Errorf("no stable archive found for %s/%s", goos, goarch)
}

// CurrentVersion returns the version of the Go toolchain actually installed
// on this machine, e.g. "go1.23.4".
//
// GOTOOLCHAIN=local forces the go command to ignore any "toolchain" directive
// in a nearby go.mod, which would otherwise report a different, auto-downloaded
// toolchain version instead of the one installed at the system Go root.
func CurrentVersion() (string, error) {
	cmd := exec.Command("go", "env", "GOVERSION")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running go env GOVERSION: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}
