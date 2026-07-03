//go:build manual

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRealArchiveExtracts is a manual, network-dependent check that Extract
// correctly handles the actual go.dev archive format (which may include
// symlinks, hardlinks, or pax headers that a synthetic test fixture would
// not exercise). It never touches /usr/local.
//
// Run explicitly: go test -tags manual -run TestRealArchiveExtracts -v ./...
func TestRealArchiveExtracts(t *testing.T) {
	releases, err := FetchReleases(defaultBaseURL)
	if err != nil {
		t.Fatalf("FetchReleases: %v", err)
	}

	_, file, err := LatestArchive(releases, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("LatestArchive: %v", err)
	}

	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, file.Filename)
	url := defaultBaseURL + "/" + file.Filename

	if err := Download(url, file.Sha256, tarPath); err != nil {
		t.Fatalf("Download: %v", err)
	}

	installRoot := t.TempDir()
	if err := Extract(tarPath, installRoot); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	goBin := filepath.Join(installRoot, "go", "bin", "go")
	out, err := exec.Command(goBin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("go version: %v (%s)", err, out)
	}
	t.Logf("extracted toolchain reports: %s", out)

	// go version alone can pass even if the install is missing files that
	// only the compiler toolchain (symlinked or not) actually needs, so
	// also build a trivial program through the extracted install.
	src := filepath.Join(tmpDir, "hello.go")
	if err := os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("writing hello.go: %v", err)
	}

	buildOut, err := exec.Command(goBin, "build", "-o", filepath.Join(tmpDir, "hello"), src).CombinedOutput()
	if err != nil {
		t.Fatalf("go build via extracted toolchain: %v (%s)", err, buildOut)
	}
}
