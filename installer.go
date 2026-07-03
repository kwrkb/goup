package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// httpClient is shared by all network calls so downloads and metadata
// fetches never hang forever on a stalled connection.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// wrapPermissionError adds a hint to rerun with sudo when err is, or wraps,
// a permission error, and passes other errors through unchanged.
//
// errors.Is (not os.IsPermission) is required here: callers pass in an err
// already wrapped by fmt.Errorf("...: %w", err), and os.IsPermission only
// unwraps *PathError/*LinkError/*SyscallError directly, not arbitrary
// Unwrap() chains, so it would never see the underlying permission error.
func wrapPermissionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w (hint: rerun with sudo)", err)
	}
	return err
}

// checkWritable probes whether the current process can create files in dir
// by creating and immediately removing a temp file. It reflects the
// effective filesystem/OS answer (ACLs, read-only mounts, uid) rather than
// guessing from stat + uid, and lets Update fail fast before spending
// bandwidth on a download that will be discarded at Backup time.
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".goup-probe-*")
	if err != nil {
		return wrapPermissionError(fmt.Errorf("checking write access to %s: %w", dir, err))
	}
	defer os.Remove(probe.Name())
	defer probe.Close()
	return nil
}

// Download fetches url, verifies its sha256 against sha256Hex while streaming
// to destPath, and removes the partial file if the fetch or verification fails.
func Download(url, sha256Hex, destPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: unexpected status %s", url, resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}

	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, hasher), resp.Body)
	closeErr := out.Close()

	if copyErr != nil {
		os.Remove(destPath)
		return fmt.Errorf("downloading %s: %w", url, copyErr)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return fmt.Errorf("closing %s: %w", destPath, closeErr)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if sum != sha256Hex {
		os.Remove(destPath)
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", url, sum, sha256Hex)
	}

	return nil
}

// findBackups returns all existing "<installRoot>/go.bak.*" paths.
func findBackups(installRoot string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(installRoot, "go.bak.*"))
	if err != nil {
		return nil, fmt.Errorf("listing backups: %w", err)
	}
	return matches, nil
}

// removeOldBackups deletes any existing backups, enforcing single-generation retention.
func removeOldBackups(installRoot string) error {
	backups, err := findBackups(installRoot)
	if err != nil {
		return err
	}
	for _, b := range backups {
		if err := os.RemoveAll(b); err != nil {
			return wrapPermissionError(fmt.Errorf("removing old backup %s: %w", b, err))
		}
	}
	return nil
}

// Backup renames "<installRoot>/go" to a timestamped backup path, first
// removing any previous backup so only the latest generation is kept.
func Backup(installRoot string) (string, error) {
	if err := removeOldBackups(installRoot); err != nil {
		return "", err
	}

	src := filepath.Join(installRoot, "go")
	backupPath := filepath.Join(installRoot, fmt.Sprintf("go.bak.%d", time.Now().Unix()))

	if err := os.Rename(src, backupPath); err != nil {
		return "", wrapPermissionError(fmt.Errorf("backing up %s: %w", src, err))
	}

	return backupPath, nil
}

// Extract unpacks a gzip-compressed tar archive (as published by go.dev/dl)
// into installRoot, rejecting any entry that would escape installRoot.
func Extract(tarGzPath, installRoot string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", tarGzPath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	root := filepath.Clean(installRoot)
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		target := filepath.Join(root, hdr.Name)
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry outside install root: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return wrapPermissionError(fmt.Errorf("creating dir %s: %w", target, err))
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return wrapPermissionError(fmt.Errorf("creating dir %s: %w", filepath.Dir(target), err))
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return wrapPermissionError(fmt.Errorf("creating file %s: %w", target, err))
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return fmt.Errorf("writing %s: %w", target, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("closing %s: %w", target, closeErr)
			}
		case tar.TypeSymlink:
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return wrapPermissionError(fmt.Errorf("creating symlink %s: %w", target, err))
			}
		default:
			// Skip other entry types (official go archives only contain dirs and regular files).
		}
	}

	return nil
}

// VerifyLaunch confirms that the Go toolchain installed at installRoot starts up.
func VerifyLaunch(installRoot string) error {
	goBin := filepath.Join(installRoot, "go", "bin", "go")

	out, err := exec.Command(goBin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("launch check failed for %s: %w (output: %s)", goBin, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// restoreFrom replaces "<installRoot>/go" with the contents of backupPath.
func restoreFrom(installRoot, backupPath string) error {
	current := filepath.Join(installRoot, "go")

	if _, err := os.Stat(current); err == nil {
		if err := os.RemoveAll(current); err != nil {
			return wrapPermissionError(fmt.Errorf("removing %s: %w", current, err))
		}
	}

	if err := os.Rename(backupPath, current); err != nil {
		return wrapPermissionError(fmt.Errorf("restoring %s: %w", backupPath, err))
	}

	return nil
}

// Rollback restores "<installRoot>/go" from the most recent backup.
func Rollback(installRoot string) error {
	backups, err := findBackups(installRoot)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backup found under %s", installRoot)
	}

	if err := checkWritable(installRoot); err != nil {
		return err
	}

	sort.Strings(backups)
	latest := backups[len(backups)-1]

	if err := restoreFrom(installRoot, latest); err != nil {
		return err
	}

	if err := VerifyLaunch(installRoot); err != nil {
		return err
	}

	restored, err := CurrentVersion(installRoot)
	if err != nil {
		return err
	}
	fmt.Printf("Rolled back to %s (from %s)\n", restored, filepath.Base(latest))
	return nil
}

// Update downloads, verifies, and installs the latest stable Go release into
// installRoot, rolling back automatically if the new install fails to launch.
func Update(installRoot, baseURL string) error {
	releases, err := FetchReleases(baseURL)
	if err != nil {
		return err
	}

	_, file, err := LatestArchive(releases, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	current, err := CurrentVersion(installRoot)
	if err != nil {
		return err
	}

	if current == file.Version {
		fmt.Printf("Already up to date: %s\n", current)
		return nil
	}

	// Probe write access before spending bandwidth on the download.
	if err := checkWritable(installRoot); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "goup-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, file.Filename)
	downloadURL := strings.TrimRight(baseURL, "/") + "/" + file.Filename

	fmt.Printf("Downloading %s ...\n", downloadURL)
	if err := Download(downloadURL, file.Sha256, tarPath); err != nil {
		return err
	}
	fmt.Println("sha256 verified")

	fmt.Printf("Backing up %s ...\n", filepath.Join(installRoot, "go"))
	backupPath, err := Backup(installRoot)
	if err != nil {
		return err
	}

	fmt.Printf("Extracting to %s ...\n", installRoot)
	if err := Extract(tarPath, installRoot); err != nil {
		fmt.Fprintf(os.Stderr, "extraction failed, rolling back: %v\n", err)
		if rerr := restoreFrom(installRoot, backupPath); rerr != nil {
			return fmt.Errorf("extraction failed AND rollback failed: %v (rollback error: %w)", err, rerr)
		}
		return fmt.Errorf("update failed during extraction, rolled back to %s: %w", current, err)
	}

	if err := VerifyLaunch(installRoot); err != nil {
		fmt.Fprintf(os.Stderr, "launch check failed, rolling back: %v\n", err)
		if rerr := restoreFrom(installRoot, backupPath); rerr != nil {
			return fmt.Errorf("launch check failed AND rollback failed: %v (rollback error: %w)", err, rerr)
		}
		return fmt.Errorf("update failed launch check, rolled back to %s: %w", current, err)
	}

	fmt.Printf("Updated: %s -> %s\n", current, file.Version)
	fmt.Printf("Backup kept at %s (use `goup rollback` to restore)\n", backupPath)

	return nil
}
