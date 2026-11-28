package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateBlock(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	original := "export EDITOR=nvim\nalias ll='ls -l'\n"
	if err := os.WriteFile(rc, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		data, err := os.ReadFile(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	block := shellBlock("zsh", "eval \"$('/opt/whatsapp-tui' completion zsh)\"")
	if err := updateBlock(rc, block, false); err != nil {
		t.Fatal(err)
	}
	first := read()
	if !strings.HasPrefix(first, original) || !strings.Contains(first, block) {
		t.Fatalf("install changed the existing lines or missed the block:\n%s", first)
	}

	// Installing again replaces the block instead of adding a second one.
	moved := shellBlock("zsh", "eval \"$('/new/whatsapp-tui' completion zsh)\"")
	if err := updateBlock(rc, moved, false); err != nil {
		t.Fatal(err)
	}
	second := read()
	if strings.Count(second, blockStart) != 1 || !strings.Contains(second, "/new/whatsapp-tui") || strings.Contains(second, "/opt/") {
		t.Fatalf("reinstall should replace the block:\n%s", second)
	}

	// Uninstall gives back the original file exactly.
	if err := updateBlock(rc, "", true); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != original {
		t.Fatalf("uninstall left %q, want %q", got, original)
	}

	// The file keeps its permissions. Windows has no Unix permission bits:
	// Go reports every writable file as 0666 there, so skip the check.
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(rc); info.Mode().Perm() != 0o600 {
			t.Fatalf("mode changed to %v", info.Mode().Perm())
		}
	}

	// A missing file is created; uninstalling with no block changes nothing.
	fresh := filepath.Join(dir, "sub", ".bashrc")
	if err := updateBlock(fresh, block, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(fresh); string(data) != block {
		t.Fatalf("new file: %q", data)
	}
	other := filepath.Join(dir, "other")
	os.WriteFile(other, []byte("no newline at end"), 0o644)
	if err := updateBlock(other, "", true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(other); string(data) != "no newline at end" {
		t.Fatalf("uninstall touched a file without a block: %q", data)
	}

	// UTF-16 files (some PowerShell profiles) are refused, not corrupted.
	utf16 := filepath.Join(dir, "profile.ps1")
	os.WriteFile(utf16, []byte{0xFF, 0xFE, 'a', 0}, 0o644)
	if err := updateBlock(utf16, block, false); err == nil {
		t.Fatal("expected UTF-16 profile to be refused")
	}
}

func TestQuote(t *testing.T) {
	if got := shQuote("/home/o'brien/whatsapp-tui"); got != `'/home/o'\''brien/whatsapp-tui'` {
		t.Errorf("shQuote: %s", got)
	}
	if got := psQuote(`C:\Users\O'Brien\whatsapp-tui.exe`); got != `'C:\Users\O''Brien\whatsapp-tui.exe'` {
		t.Errorf("psQuote: %s", got)
	}
}
