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
		wantErr bool
	}{
		{name: "version only", args: []string{"1.26.3"}, version: "1.26.3"},
		{name: "flag before version", args: []string{"--pre", "1.27rc1"}, version: "1.27rc1", pre: true},
		// This is the case Codex flagged: --pre appears AFTER the positional
		// argument. Go's flag package normally stops at the first non-flag
		// token, so this used to fail with "install requires exactly one
		// version argument".
		{name: "flag after version", args: []string{"1.27rc1", "--pre"}, version: "1.27rc1", pre: true},
		{name: "no args", args: nil, wantErr: true},
		{name: "two positionals", args: []string{"1.26.3", "1.24.5"}, wantErr: true},
		{name: "unknown flag", args: []string{"1.26.3", "--nope"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, p, err := parseInstallArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got version=%q pre=%v", v, p)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInstallArgs: %v", err)
			}
			if v != c.version || p != c.pre {
				t.Fatalf("got (version=%q, pre=%v), want (version=%q, pre=%v)", v, p, c.version, c.pre)
			}
		})
	}
}
