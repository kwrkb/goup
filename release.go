package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

// FetchReleases retrieves the recent Go release list from baseURL
// (e.g. https://go.dev/dl). Only the last few stable releases are returned;
// use FetchAllReleases when the full history (including pre-releases) is
// needed.
func FetchReleases(baseURL string) ([]Release, error) {
	return fetchReleases(strings.TrimRight(baseURL, "/") + "/?mode=json")
}

// FetchAllReleases retrieves the full Go release history from baseURL,
// including pre-releases. Used by `goup list` and `goup install <version>`
// where matching against an older or non-stable release matters.
func FetchAllReleases(baseURL string) ([]Release, error) {
	return fetchReleases(strings.TrimRight(baseURL, "/") + "/?mode=json&include=all")
}

func fetchReleases(url string) ([]Release, error) {
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

// FindArchive returns the release and its archive matching an exact
// wantVersion string (e.g. "go1.26.3") for goos/goarch. Used by
// `goup install <version>`.
func FindArchive(releases []Release, wantVersion, goos, goarch string) (Release, ReleaseFile, error) {
	for _, r := range releases {
		if r.Version != wantVersion {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == goos && f.Arch == goarch {
				return r, f, nil
			}
		}
		return Release{}, ReleaseFile{}, fmt.Errorf("release %s has no archive for %s/%s", wantVersion, goos, goarch)
	}
	return Release{}, ReleaseFile{}, fmt.Errorf("release %s not found (see `goup list --all` for available versions)", wantVersion)
}

// NormalizeVersion accepts "1.26.3" or "go1.26.3" (and pre-release variants
// like "1.27rc1") and returns the canonical "go..."-prefixed form.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "go") {
		return v
	}
	return "go" + v
}

// CurrentVersion returns the version of the Go toolchain installed at
// installRoot, e.g. "go1.23.4".
//
// It reads "<installRoot>/go/VERSION" directly, rather than shelling out to
// `go env GOVERSION`, because (1) sudo strips $PATH so the installed go
// binary is not resolvable when goup is invoked as `sudo goup update`, and
// (2) reading the VERSION file bypasses the go.mod toolchain-directive
// autodownload that would otherwise report a different, transient version.
func CurrentVersion(installRoot string) (string, error) {
	versionPath := filepath.Join(installRoot, "go", "VERSION")

	data, err := os.ReadFile(versionPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", versionPath, err)
	}

	// The VERSION file's first line is the version (e.g. "go1.26.3");
	// subsequent lines carry build metadata that we don't care about.
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line), nil
}
