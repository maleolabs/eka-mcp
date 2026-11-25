package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestThemeMarginWriter: non-blank lines gain the 2-space margin,
// blank lines stay truly blank, \r redraws pass through.
func TestThemeMarginWriter(t *testing.T) {
	var buf bytes.Buffer
	mw := newThemeMarginWriter(&buf)
	if _, err := mw.Write([]byte("a\n\nb\n")); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "  a\n\n  b\n"; got != want {
		t.Errorf("margin = %q, want %q", got, want)
	}
	buf.Reset()
	mw2 := newThemeMarginWriter(&buf)
	if _, err := mw2.Write([]byte("\rframe")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "\rframe" {
		t.Errorf("redraw must pass through, got %q", buf.String())
	}
}

// TestConfigureHumanDefault: without --json the human sections render
// (dry-run, temp dir — nothing is written).
func TestConfigureHumanDefault(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	err := runConfigure([]string{"--target", "opencode", "--dir", dir, "--dry-run"}, &buf)
	if err != nil {
		t.Fatalf("configure human: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Target", "MCP entry", "opencode", "would write"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output must contain %q:\n%s", want, out)
		}
	}
	for _, ln := range strings.Split(out, "\n") {
		if ln != "" && !strings.HasPrefix(ln, themeMargin) {
			t.Errorf("line sticks to the left edge: %q", ln)
		}
	}
}

// TestConfigureHumanWithSkills: the Install section reports counts.
func TestConfigureHumanWithSkills(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	err := runConfigure([]string{"--target", "opencode", "--dir", dir, "--with-skills", "--dry-run"}, &buf)
	if err != nil {
		t.Fatalf("configure human: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Install", "skills", "create"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output must contain %q:\n%s", want, out)
		}
	}
}
