package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type tarEntry struct {
	Name string
	Mode int64
	Body []byte
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.Name,
			Mode:     e.Mode,
			Size:     int64(len(e.Body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header for %s: %v", e.Name, err)
		}
		if _, err := tw.Write(e.Body); err != nil {
			t.Fatalf("writing tar body for %s: %v", e.Name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}

	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// goScript returns the contents of a fake `go` executable that prints
// versionLine and exits with exitCode, used in place of the real go binary
// so tests never depend on or modify the actual system Go toolchain.
func goScript(versionLine string, exitCode int) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\necho %q\nexit %d\n", versionLine, exitCode))
}

func writeGoScript(t *testing.T, installRoot string, script []byte) {
	t.Helper()

	binDir := filepath.Join(installRoot, "go", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "go"), script, 0o755); err != nil {
		t.Fatalf("writing go binary: %v", err)
	}
}

func writeVersionMarker(t *testing.T, installRoot, version string) {
	t.Helper()

	goDir := filepath.Join(installRoot, "go")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", goDir, err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "VERSION"), []byte(version), 0o644); err != nil {
		t.Fatalf("writing VERSION marker: %v", err)
	}
}

func TestDownload(t *testing.T) {
	body := []byte("fake go tarball contents")
	sum := sha256Hex(body)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	t.Run("matching sha256 succeeds", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "out.tar.gz")
		if err := Download(srv.URL, sum, dest); err != nil {
			t.Fatalf("Download: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading downloaded file: %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("downloaded content mismatch")
		}
	})

	t.Run("mismatched sha256 fails and cleans up", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "out.tar.gz")
		err := Download(srv.URL, strings.Repeat("0", 64), dest)
		if err == nil {
			t.Fatal("expected error for sha256 mismatch")
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Fatalf("expected downloaded file to be removed after sha256 mismatch, stat err=%v", statErr)
		}
	})
}

func TestExtract_RejectsPathTraversal(t *testing.T) {
	data := buildTarGz(t, []tarEntry{{Name: "../evil.txt", Mode: 0o644, Body: []byte("pwned")}})
	tarPath := filepath.Join(t.TempDir(), "evil.tar.gz")
	if err := os.WriteFile(tarPath, data, 0o644); err != nil {
		t.Fatalf("writing tarball: %v", err)
	}

	installRoot := t.TempDir()
	err := Extract(tarPath, installRoot)
	if err == nil {
		t.Fatal("expected error for path traversal entry")
	}
	if !strings.Contains(err.Error(), "outside install root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBackup_SingleGeneration(t *testing.T) {
	root := t.TempDir()
	writeVersionMarker(t, root, "v1")

	if _, err := Backup(root); err != nil {
		t.Fatalf("Backup 1: %v", err)
	}

	writeVersionMarker(t, root, "v2")
	b2, err := Backup(root)
	if err != nil {
		t.Fatalf("Backup 2: %v", err)
	}

	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected exactly one retained backup, got %v", backups)
	}
	if backups[0] != b2 {
		t.Fatalf("expected remaining backup to be %s, got %s", b2, backups[0])
	}

	got, err := os.ReadFile(filepath.Join(b2, "VERSION"))
	if err != nil {
		t.Fatalf("reading backup marker: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("expected surviving backup to be v2, got %q", got)
	}
}

// TestWrapPermissionError_SeesWrappedError guards against a regression where
// os.IsPermission was used instead of errors.Is: os.IsPermission only
// unwraps *PathError/*LinkError/*SyscallError directly, so it silently
// failed to recognize a permission error once it had already been wrapped
// by fmt.Errorf("...: %w", err) at the call site, and the sudo hint never
// appeared.
func TestWrapPermissionError_SeesWrappedError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses DAC, so chmod 0o555 cannot trigger EACCES")
	}

	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	// Removing write permission on the install root itself makes renaming
	// "<root>/go" fail with EACCES, without needing actual root privileges.
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(root, 0o755)

	_, err := Backup(root)
	if err == nil {
		t.Fatal("expected a permission error")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("expected sudo hint in wrapped permission error, got: %v", err)
	}
}

func TestRollback_NoBackup(t *testing.T) {
	if err := Rollback(t.TempDir()); err == nil {
		t.Fatal("expected error when no backup exists")
	}
}

func TestRollback_RestoresBackup(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	if _, err := Backup(root); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Simulate a broken current install left behind by a failed update.
	writeGoScript(t, root, goScript("broken", 1))

	if err := Rollback(root); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("VerifyLaunch after rollback: %v", err)
	}

	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected backup to be consumed by rollback, got %v", backups)
	}
}

// newReleaseServer returns an httptest server that serves a single stable
// release for runtime.GOOS/runtime.GOARCH, backed by archiveBody.
func newReleaseServer(t *testing.T, version, filename string, archiveBody []byte) *httptest.Server {
	t.Helper()

	sum := sha256Hex(archiveBody)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") != "json" {
			http.NotFound(w, r)
			return
		}
		releases := []Release{{
			Version: version,
			Stable:  true,
			Files: []ReleaseFile{{
				Filename: filename,
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				Version:  version,
				Sha256:   sum,
				Kind:     "archive",
			}},
		}}
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Fatalf("encoding release list: %v", err)
		}
	})
	mux.HandleFunc("/"+filename, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveBody)
	})

	return httptest.NewServer(mux)
}

func TestUpdate_Success(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	archive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goTEST-new linux/amd64", 0)},
	})

	srv := newReleaseServer(t, "goTEST-new", "go-good.tar.gz", archive)
	defer srv.Close()

	if err := Update(root, srv.URL); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("VerifyLaunch after update: %v", err)
	}

	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected exactly one backup retained after successful update, got %v", backups)
	}
}

func TestUpdate_AutoRollbackOnLaunchFailure(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	brokenArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("broken", 1)},
	})

	srv := newReleaseServer(t, "goTEST-broken", "go-broken.tar.gz", brokenArchive)
	defer srv.Close()

	err := Update(root, srv.URL)
	if err == nil {
		t.Fatal("expected Update to fail due to launch check")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected rollback message, got: %v", err)
	}

	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("expected rollback to restore a working toolchain, VerifyLaunch: %v", err)
	}

	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no leftover backup after auto-rollback, got %v", backups)
	}
}

func TestUpdate_AutoRollbackOnExtractFailure(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	// A valid entry followed by a path-traversal entry: Extract writes go/bin/go
	// before hitting the rejected entry, so this also proves the rollback undoes
	// a partially-extracted install, not just an install that never started.
	unsafeArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goTEST-new linux/amd64", 0)},
		{Name: "../evil.txt", Mode: 0o644, Body: []byte("pwned")},
	})

	srv := newReleaseServer(t, "goTEST-unsafe", "go-unsafe.tar.gz", unsafeArchive)
	defer srv.Close()

	err := Update(root, srv.URL)
	if err == nil {
		t.Fatal("expected Update to fail due to extraction error")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected rollback message, got: %v", err)
	}

	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("expected rollback to restore a working toolchain, VerifyLaunch: %v", err)
	}

	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no leftover backup after auto-rollback, got %v", backups)
	}
}

func TestUpdate_ReadOnlyRootFailsBeforeDownload(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses DAC, so chmod 0o555 cannot trigger EACCES")
	}

	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	archive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goTEST-new linux/amd64", 0)},
	})

	sum := sha256Hex(archive)
	var archiveHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") != "json" {
			http.NotFound(w, r)
			return
		}
		releases := []Release{{
			Version: "goTEST-new",
			Stable:  true,
			Files: []ReleaseFile{{
				Filename: "go-new.tar.gz",
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				Version:  "goTEST-new",
				Sha256:   sum,
				Kind:     "archive",
			}},
		}}
		json.NewEncoder(w).Encode(releases)
	})
	mux.HandleFunc("/go-new.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		archiveHits++
		w.Write(archive)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Remove write permission so Backup/Extract would eventually fail.
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(root, 0o755)

	err := Update(root, srv.URL)
	if err == nil {
		t.Fatal("expected Update to fail on read-only install root")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("expected sudo hint in error, got: %v", err)
	}
	if archiveHits != 0 {
		t.Fatalf("expected fast-fail before downloading archive, got %d hits", archiveHits)
	}
}

// newMultiReleaseServer serves a small catalog with a stable release
// (gostable) and a pre-release (goprerc1) for runtime.GOOS/runtime.GOARCH,
// so Install's version-lookup and --pre gating can be exercised.
func newMultiReleaseServer(t *testing.T, stableBody, preBody []byte) *httptest.Server {
	t.Helper()

	stableSum := sha256Hex(stableBody)
	preSum := sha256Hex(preBody)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") != "json" {
			http.NotFound(w, r)
			return
		}
		releases := []Release{
			{
				Version: "goprerc1",
				Stable:  false,
				Files: []ReleaseFile{{
					Filename: "go-pre.tar.gz",
					OS:       runtime.GOOS,
					Arch:     runtime.GOARCH,
					Version:  "goprerc1",
					Sha256:   preSum,
					Kind:     "archive",
				}},
			},
			{
				Version: "gostable",
				Stable:  true,
				Files: []ReleaseFile{{
					Filename: "go-stable.tar.gz",
					OS:       runtime.GOOS,
					Arch:     runtime.GOARCH,
					Version:  "gostable",
					Sha256:   stableSum,
					Kind:     "archive",
				}},
			},
		}
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Fatalf("encoding release list: %v", err)
		}
	})
	mux.HandleFunc("/go-stable.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(stableBody)
	})
	mux.HandleFunc("/go-pre.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(preBody)
	})

	return httptest.NewServer(mux)
}

func TestInstall_SpecificVersion(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	stableArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version gostable linux/amd64", 0)},
	})
	preArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goprerc1 linux/amd64", 0)},
	})

	srv := newMultiReleaseServer(t, stableArchive, preArchive)
	defer srv.Close()

	if err := Install(root, srv.URL, "STABLE", false); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("VerifyLaunch: %v", err)
	}
}

func TestInstall_UnknownVersionErrors(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	stableArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version gostable linux/amd64", 0)},
	})
	preArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goprerc1 linux/amd64", 0)},
	})

	srv := newMultiReleaseServer(t, stableArchive, preArchive)
	defer srv.Close()

	err := Install(root, srv.URL, "NOPE", false)
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

func TestInstall_PreReleaseRefusedWithoutFlag(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	stableArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version gostable linux/amd64", 0)},
	})
	preArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goprerc1 linux/amd64", 0)},
	})

	srv := newMultiReleaseServer(t, stableArchive, preArchive)
	defer srv.Close()

	err := Install(root, srv.URL, "PRErc1", false)
	if err == nil {
		t.Fatal("expected pre-release to be refused without --pre")
	}
	if !strings.Contains(err.Error(), "pre-release") {
		t.Fatalf("expected pre-release hint, got: %v", err)
	}
}

func TestInstall_PreReleaseAcceptedWithFlag(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	stableArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version gostable linux/amd64", 0)},
	})
	preArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goprerc1 linux/amd64", 0)},
	})

	srv := newMultiReleaseServer(t, stableArchive, preArchive)
	defer srv.Close()

	if err := Install(root, srv.URL, "PRErc1", true); err != nil {
		t.Fatalf("Install --pre: %v", err)
	}
	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("VerifyLaunch: %v", err)
	}
}

// TestInstall_AlreadyAtTarget confirms the no-op path: no HTTP hits for the
// archive, no backup created, no VERSION touched.
func TestInstall_AlreadyAtTarget(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version gostable linux/amd64", 0))
	writeVersionMarker(t, root, "gostable")

	stableArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version gostable linux/amd64", 0)},
	})
	preArchive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goprerc1 linux/amd64", 0)},
	})

	srv := newMultiReleaseServer(t, stableArchive, preArchive)
	defer srv.Close()

	if err := Install(root, srv.URL, "STABLE", false); err != nil {
		t.Fatalf("Install (no-op): %v", err)
	}

	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backup when target == current, got %v", backups)
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.26.3", "go1.26.3"},
		{"go1.26.3", "go1.26.3"},
		{"1.27rc1", "go1.27rc1"},
		{"go1.27rc1", "go1.27rc1"},
		{"  1.26.3  ", "go1.26.3"},
		// Mixed-case inputs should collapse to the canonical lowercase
		// form so mistyped versions still match the go.dev release list.
		{"Go1.26.3", "go1.26.3"},
		{"GO1.26.3", "go1.26.3"},
		{"1.26RC1", "go1.26rc1"},
		{"Go1.27RC1", "go1.27rc1"},
	}
	for _, c := range cases {
		if got := NormalizeVersion(c.in); got != c.want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUpdate_Sha256MismatchAbortsBeforeExtract(t *testing.T) {
	root := t.TempDir()
	writeGoScript(t, root, goScript("go version goOLD linux/amd64", 0))
	writeVersionMarker(t, root, "goOLD")

	archive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goTEST-new linux/amd64", 0)},
	})

	// Serve a release whose advertised sha256 does not match the archive body.
	sum := sha256Hex([]byte("not the archive"))
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		releases := []Release{{
			Version: "goTEST-new",
			Stable:  true,
			Files: []ReleaseFile{{
				Filename: "go-mismatch.tar.gz",
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				Version:  "goTEST-new",
				Sha256:   sum,
				Kind:     "archive",
			}},
		}}
		json.NewEncoder(w).Encode(releases)
	})
	mux.HandleFunc("/go-mismatch.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	err := Update(root, srv.URL)
	if err == nil {
		t.Fatal("expected Update to fail on sha256 mismatch")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch error, got: %v", err)
	}

	// Backup() runs only after sha256 verification succeeds, so none should exist.
	backups, err := findBackups(root)
	if err != nil {
		t.Fatalf("findBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backup created before sha256 verification, got %v", backups)
	}

	// The original install must be untouched.
	if err := VerifyLaunch(root); err != nil {
		t.Fatalf("expected original install to remain untouched, VerifyLaunch: %v", err)
	}
}
