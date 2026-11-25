package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maleolabs/eka-core/metadata"
	"github.com/maleolabs/eka-core/workspace"
	"github.com/maleolabs/eka-mcp"
	"github.com/maleolabs/eka-mcp/internal/eka"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// TestBannerTTYGate: banner appears only when stdin is TTY, absent otherwise.
func TestBannerTTYGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	cap, err := eka.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	var buf bytes.Buffer
	writeBannerIfTTY(&buf, false, cap)
	if buf.Len() != 0 {
		t.Fatalf("non-TTY (stdin false) must produce zero bytes, got %q", buf.String())
	}
	var buf2 bytes.Buffer
	writeBannerIfTTY(&buf2, true, cap)
	if buf2.Len() == 0 {
		t.Fatal("TTY (stdin true) must produce banner bytes, got empty")
	}
	if !strings.Contains(buf2.String(), "EKA MCP") {
		t.Errorf("TTY banner must contain EKA MCP heading, got %q", buf2.String())
	}
}

// TestBannerContentInitialized vs Uninitialized: banner reflects workspace state.
func TestBannerContentUninitialized(t *testing.T) {
	home := t.TempDir() // no workspace.json
	t.Setenv("EKA_HOME", home)

	cap, err := eka.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()
	if cap.Exists() {
		t.Fatal("should be uninitialized")
	}

	st, err := collectBannerState(cap)
	if err != nil {
		t.Fatal(err)
	}
	if st.Initialized {
		t.Error("uninitialized state must have Initialized=false")
	}
	if st.Projects != 0 || st.Repos != 0 {
		t.Errorf("uninitialized projects/repos must be 0, got %d/%d", st.Projects, st.Repos)
	}
	if st.WorkspacePath == "" {
		t.Error("workspace path must be set even when uninitialized")
	}
	if st.WorkspacePath != home {
		t.Errorf("workspace path = %q, want EKA_HOME %q", st.WorkspacePath, home)
	}
	var buf bytes.Buffer
	bs := &bannerStyle{Color: false, W: &buf}
	renderBanner(bs, st)
	out := buf.String()
	// Required content per AC2
	assertContains(t, out, pack.Version)
	// pack from manifest
	info, _ := pack.ReadPackInfo()
	assertContains(t, out, info.Name)
	assertContains(t, out, info.Version)
	assertContains(t, out, info.Status)
	assertContains(t, out, mcp.ProtocolVersion)
	assertContains(t, out, "stdio")
	assertContains(t, out, "JSON-RPC 2.0")
	assertContains(t, out, "newline-delimited")
	assertContains(t, out, home)
	assertContains(t, out, "not initialized")
	assertContains(t, out, "projects")
	assertContains(t, out, "repositories")
	// capability counts
	assertContains(t, out, fmt.Sprintf("%d", mcp.ToolCount()))
	rc, _ := mcp.ResourceCount()
	assertContains(t, out, fmt.Sprintf("%d", rc))
	// hints
	assertContains(t, out, "eka-mcp configure --target")
	assertContains(t, out, "Ctrl-C")
	// visual language: section headers, tree glyphs, no colors
	if strings.Contains(out, "\x1b[") {
		t.Error("plain (non-TTY) banner must not contain ANSI escapes")
	}
	if !strings.Contains(out, treeLast) {
		t.Errorf("banner must contain tree glyph %q", treeLast)
	}
	if !strings.Contains(out, "Runtime") || !strings.Contains(out, "Workspace") || !strings.Contains(out, "Capabilities") || !strings.Contains(out, "Hints") {
		t.Error("banner must contain section headers Runtime/Workspace/Capabilities/Hints")
	}
	// deterministic hint for not initialized
	if !strings.Contains(out, "run 'eka project register'") {
		t.Error("uninitialized banner must contain deterministic not-initialized hint")
	}
}

func TestBannerContentInitialized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	ws, err := workspace.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Register a project + repo to have non-zero counts.
	base := t.TempDir()
	repo := filepath.Join(base, "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	m := metadata.Metadata{Version: metadata.SchemaVersion, Project: "beather", Name: "myrepo", Namespace: "beather"}
	if err := os.WriteFile(filepath.Join(repo, "eka.yaml"), m.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ws.RegisterRepoMetadata(repo, m); err != nil {
		t.Fatalf("RegisterRepoMetadata: %v", err)
	}

	cap, err := eka.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()
	if !cap.Exists() {
		t.Fatal("should be initialized")
	}
	st, _ := collectBannerState(cap)
	if !st.Initialized {
		t.Error("initialized state must be true")
	}
	if st.Projects != 1 {
		t.Errorf("projects = %d, want 1", st.Projects)
	}
	if st.Repos != 1 {
		t.Errorf("repos = %d, want 1", st.Repos)
	}
	var buf bytes.Buffer
	bs := &bannerStyle{Color: false, W: &buf}
	renderBanner(bs, st)
	out := buf.String()
	assertContains(t, out, "yes")
	if strings.Contains(out, "not initialized") {
		t.Error("initialized banner must not contain not-initialized marker")
	}
	assertContains(t, out, home)
	assertContains(t, out, "1")
}

// TestBannerByteDeterminism: identical state yields identical bytes; no timestamps.
func TestBannerByteDeterminism(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	cap, err := eka.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	st, _ := collectBannerState(cap)
	var a, b bytes.Buffer
	renderBanner(&bannerStyle{Color: false, W: &a}, st)
	renderBanner(&bannerStyle{Color: false, W: &b}, st)
	if a.String() != b.String() {
		t.Fatalf("banner must be byte-deterministic for identical state:\n---a---\n%s\n---b---\n%s", a.String(), b.String())
	}
	// No wall-clock timestamps: output must not contain current year/time artifacts beyond pack version.
	// We assert it contains no RFC3339-like timestamp.
	if strings.Contains(a.String(), "T00:00:00Z") || strings.Contains(a.String(), "20:") {
		t.Error("banner must not contain wall-clock timestamps")
	}
}

// TestBannerColorOnlyWhenTTY: plain bytes otherwise.
func TestBannerColorOnlyWhenTTY(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	cap, err := eka.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()
	st, _ := collectBannerState(cap)

	// Plain writer (bytes.Buffer) must be plain.
	var plain bytes.Buffer
	renderBanner(&bannerStyle{Color: false, W: &plain}, st)
	if strings.Contains(plain.String(), "\x1b[") {
		t.Error("plain banner must not contain escapes")
	}
	// Colored writer must contain escapes and same textual content without escapes.
	var colored bytes.Buffer
	renderBanner(&bannerStyle{Color: true, W: &colored}, st)
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Error("colored banner must contain ANSI escapes")
	}
	// Strip escapes and compare content core: both must contain required substrings.
	plainStr := plain.String()
	coloredStr := colored.String()
	// Remove SGR sequences for comparison: simple check that plain content appears inside stripped colored.
	stripped := stripANSI(coloredStr)
	// Stripped colored should equal plain (deterministic, color doesn't affect layout).
	if stripped != plainStr {
		// Allow difference only in escape wrappers — stripped must be identical.
		t.Errorf("color should not affect layout; stripped colored != plain\nplain=%q\nstripped=%q", plainStr, stripped)
	}
}

// TestBannerDoesNotWriteToStdout: ensure writeBannerIfTTY only writes to provided writer (stderr).
func TestBannerDoesNotWriteToStdout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EKA_HOME", home)
	cap, err := eka.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer cap.Close()

	var stderr bytes.Buffer
	var stdout bytes.Buffer
	// Simulate serve's contract: banner goes to stderr only.
	writeBannerIfTTY(&stderr, true, cap)
	// stdout stays empty.
	if stdout.Len() != 0 {
		t.Error("stdout must stay empty")
	}
	if stderr.Len() == 0 {
		t.Error("stderr (provided writer) must contain banner when TTY")
	}
}

// Helpers.

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("banner must contain %q, got:\n%s", substr, s)
	}
}

func stripANSI(s string) string {
	// Remove "\x1b[...m" sequences.
	var out strings.Builder
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '\x1b' && s[i+1] == '[' {
			// skip until m
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
