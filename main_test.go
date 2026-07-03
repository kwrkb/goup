package main

import (
	"bytes"
	"strings"
	"testing"
)

func mkReleases() []Release {
	return []Release{
		{Version: "go1.27rc1", Stable: false},
		{Version: "go1.26.4", Stable: true},
		{Version: "go1.26.3", Stable: true},
		{Version: "go1.26.2", Stable: true},
		{Version: "go1.26.1", Stable: true},
		{Version: "go1.26.0", Stable: true},
		{Version: "go1.25.5", Stable: true},
	}
}

func TestRenderReleaseList_StableOnlyByDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReleaseList(&buf, mkReleases(), "go1.26.3", false, 10); err != nil {
		t.Fatalf("renderReleaseList: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "go1.27rc1") {
		t.Errorf("expected pre-release to be hidden by default, got:\n%s", out)
	}
	if !strings.Contains(out, "* go1.26.3") {
		t.Errorf("expected current version marker on go1.26.3, got:\n%s", out)
	}
	if !strings.Contains(out, "go1.26.4") {
		t.Errorf("expected latest stable in output, got:\n%s", out)
	}
}

func TestRenderReleaseList_AllIncludesPreRelease(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReleaseList(&buf, mkReleases(), "", true, 10); err != nil {
		t.Fatalf("renderReleaseList: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "go1.27rc1") {
		t.Errorf("expected pre-release with --all, got:\n%s", out)
	}
	if !strings.Contains(out, "pre-release") {
		t.Errorf("expected pre-release kind label with --all, got:\n%s", out)
	}
}

func TestRenderReleaseList_LimitTruncates(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReleaseList(&buf, mkReleases(), "", false, 2); err != nil {
		t.Fatalf("renderReleaseList: %v", err)
	}
	lines := strings.Count(buf.String(), "\n")
	if lines != 2 {
		t.Errorf("expected 2 lines with -n 2, got %d:\n%s", lines, buf.String())
	}
}

// TestRenderReleaseList_CurrentOutOfWindow guards the "..." fallback that
// surfaces the installed toolchain when it falls outside the -n limit, so
// `goup list` never hides what you are running.
func TestRenderReleaseList_CurrentOutOfWindow(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReleaseList(&buf, mkReleases(), "go1.25.5", false, 2); err != nil {
		t.Fatalf("renderReleaseList: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "...") {
		t.Errorf("expected ellipsis when current is outside limit, got:\n%s", out)
	}
	if !strings.Contains(out, "* go1.25.5") {
		t.Errorf("expected current marker on out-of-window version, got:\n%s", out)
	}
}

func TestRenderReleaseList_EmptyIsError(t *testing.T) {
	var buf bytes.Buffer
	err := renderReleaseList(&buf, []Release{{Version: "go1.27rc1", Stable: false}}, "", false, 10)
	if err == nil {
		t.Fatal("expected error when nothing matches the filter")
	}
}

// TestRenderReleaseList_NoEllipsisWhenCurrentMissing guards against a
// dangling "..." line when the current version is not in the fetched
// release list at all (dev builds, ancient versions dropped from
// go.dev). The ellipsis must only appear when a matching row follows.
func TestRenderReleaseList_NoEllipsisWhenCurrentMissing(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReleaseList(&buf, mkReleases(), "goANCIENT", false, 2); err != nil {
		t.Fatalf("renderReleaseList: %v", err)
	}
	if strings.Contains(buf.String(), "...") {
		t.Errorf("expected no ellipsis when current is not in releases, got:\n%s", buf.String())
	}
}

func TestParseInstallArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		version string
		pre     bool
		noSudo  bool
		wantErr bool
	}{
		{name: "version only", args: []string{"1.26.3"}, version: "1.26.3"},
		{name: "flag before version", args: []string{"--pre", "1.27rc1"}, version: "1.27rc1", pre: true},
		// This is the case Codex flagged: --pre appears AFTER the positional
		// argument. Go's flag package normally stops at the first non-flag
		// token, so this used to fail with "install requires exactly one
		// version argument".
		{name: "flag after version", args: []string{"1.27rc1", "--pre"}, version: "1.27rc1", pre: true},
		{name: "no-sudo before version", args: []string{"--no-sudo", "1.26.3"}, version: "1.26.3", noSudo: true},
		{name: "no-sudo after version", args: []string{"1.26.3", "--no-sudo"}, version: "1.26.3", noSudo: true},
		{name: "both flags", args: []string{"--pre", "1.27rc1", "--no-sudo"}, version: "1.27rc1", pre: true, noSudo: true},
		{name: "no args", args: nil, wantErr: true},
		{name: "two positionals", args: []string{"1.26.3", "1.24.5"}, wantErr: true},
		{name: "unknown flag", args: []string{"1.26.3", "--nope"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, p, ns, err := parseInstallArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got version=%q pre=%v noSudo=%v", v, p, ns)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInstallArgs: %v", err)
			}
			if v != c.version || p != c.pre || ns != c.noSudo {
				t.Fatalf("got (version=%q, pre=%v, noSudo=%v), want (version=%q, pre=%v, noSudo=%v)",
					v, p, ns, c.version, c.pre, c.noSudo)
			}
		})
	}
}

// TestIsAlreadyLatest guards the pre-flight peek that runUpdate uses to
// skip sudo elevation when no write would occur. The regression it
// prevents: pre-v0.3.0 `goup update` was a no-op without sudo when the
// installed toolchain already matched the latest release, but v0.3.0's
// CLI-layer maybeElevate initially ran before the no-op check inside
// Update() and started prompting for a password on every invocation.
func TestIsAlreadyLatest(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{
		{Name: "go/bin/go", Mode: 0o755, Body: goScript("go version goLATEST", 0)},
	})
	srv := newReleaseServer(t, "goLATEST", "go-latest.tar.gz", archive)
	defer srv.Close()

	t.Run("current matches latest", func(t *testing.T) {
		root := t.TempDir()
		writeVersionMarker(t, root, "goLATEST")
		if !isAlreadyLatest(root, srv.URL) {
			t.Errorf("expected true when current == latest")
		}
	})

	t.Run("current is older", func(t *testing.T) {
		root := t.TempDir()
		writeVersionMarker(t, root, "goOLD")
		if isAlreadyLatest(root, srv.URL) {
			t.Errorf("expected false when current != latest")
		}
	})

	// No VERSION marker: fall through so the real Update surfaces the
	// underlying error instead of silently short-circuiting.
	t.Run("missing VERSION file falls through", func(t *testing.T) {
		root := t.TempDir()
		if isAlreadyLatest(root, srv.URL) {
			t.Errorf("expected false when VERSION file missing")
		}
	})
}

func TestHasHelpFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"empty", nil, false},
		{"only version arg", []string{"1.26.3"}, false},
		{"double dash", []string{"--help"}, true},
		{"single dash long", []string{"-help"}, true},
		{"short", []string{"-h"}, true},
		{"help after positional", []string{"1.26.3", "--help"}, true},
		{"help mixed with flags", []string{"--pre", "1.27rc1", "-h"}, true},
		// A literal 'help' subword must NOT trigger — otherwise
		// `goup install help` (a nonsense version) would silently
		// print help instead of erroring.
		{"bare help word", []string{"help"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasHelpFlag(c.args); got != c.want {
				t.Fatalf("hasHelpFlag(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestPrintHelpFor_KnownAndUnknown(t *testing.T) {
	var buf bytes.Buffer
	if ok := printHelpFor(&buf, "install"); !ok {
		t.Fatal("printHelpFor(install) returned false")
	}
	if !strings.Contains(buf.String(), "<version>") {
		t.Errorf("install help missing <version> in signature:\n%s", buf.String())
	}

	buf.Reset()
	if ok := printHelpFor(&buf, "totally-bogus"); ok {
		t.Fatal("printHelpFor(unknown) should return false")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for unknown command, got:\n%s", buf.String())
	}
}

func TestParseWriteFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		noSudo  bool
		wantErr bool
	}{
		{name: "empty", args: nil, noSudo: false},
		{name: "no-sudo", args: []string{"--no-sudo"}, noSudo: true},
		{name: "positional rejected", args: []string{"1.26.3"}, wantErr: true},
		{name: "unknown flag", args: []string{"--pre"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseWriteFlags("update", c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got noSudo=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWriteFlags: %v", err)
			}
			if got != c.noSudo {
				t.Fatalf("got noSudo=%v, want %v", got, c.noSudo)
			}
		})
	}
}
