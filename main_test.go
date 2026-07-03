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
