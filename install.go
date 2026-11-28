package main

// `whatsapp-tui completion install|uninstall`: turn Tab completion on or off
// for the user's shell in one step, so nobody has to edit shell config files
// by hand. The setup is a clearly marked block that can be removed again.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	blockStart = "# >>> whatsapp-tui completion >>>"
	blockEnd   = "# <<< whatsapp-tui completion <<<"
	fishHeader = "# fish completion for whatsapp-tui."
)

// runCompletionInstall adds (or with uninstall, removes) Tab completion for
// a shell: the one given, or the user's current shell.
func runCompletionInstall(args []string, uninstall bool) int {
	shell := ""
	if len(args) > 0 {
		shell = strings.ToLower(args[0])
	} else {
		shell = currentShell()
	}
	if shell == "pwsh" {
		shell = "powershell"
	}

	exe, err := executablePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌ Could not find this program's location:", err)
		return 1
	}

	var (
		done    []string // what was changed, for the summary
		notes   []string
		failure error
	)
	switch shell {
	case "zsh":
		rc := filepath.Join(zshDir(), ".zshrc")
		block := shellBlock("zsh", fmt.Sprintf(`if [[ -x %[1]s ]]; then
  (( $+functions[compdef] )) || { autoload -Uz compinit && compinit }
  eval "$(%[1]s completion zsh)"
fi`, shQuote(exe)))
		failure = updateBlock(rc, block, uninstall)
		done = append(done, tildePath(rc))
	case "bash":
		rc := filepath.Join(homeDir(), ".bashrc")
		block := shellBlock("bash", fmt.Sprintf(`if [ -x %[1]s ]; then
  eval "$(%[1]s completion bash)"
fi`, shQuote(exe)))
		failure = updateBlock(rc, block, uninstall)
		done = append(done, tildePath(rc))
	case "fish":
		file := filepath.Join(homeDir(), ".config", "fish", "completions", "whatsapp-tui.fish")
		failure = updateFishFile(file, uninstall)
		done = append(done, tildePath(file))
	case "powershell":
		profiles := powershellProfiles()
		if len(profiles) == 0 {
			failure = fmt.Errorf("PowerShell was not found")
			break
		}
		block := shellBlock("powershell", fmt.Sprintf(
			"if (Test-Path -LiteralPath %[1]s) { & %[1]s completion powershell | Out-String | Invoke-Expression }",
			psQuote(exe)))
		for _, profile := range profiles {
			if err := updateBlock(profile, block, uninstall); err != nil {
				failure = err
				break
			}
			done = append(done, profile)
		}
		if !uninstall && failure == nil && powershellBlocksProfiles() {
			notes = append(notes,
				"PowerShell on this computer is set to block profile scripts, so completion won't load yet.",
				"To allow your own scripts, run this once in PowerShell:",
				"    Set-ExecutionPolicy -Scope CurrentUser RemoteSigned")
		}
	case "":
		fmt.Fprintln(os.Stderr, "❌ Couldn't tell which shell you use. Name it: whatsapp-tui completion install zsh|bash|fish|powershell")
		return 2
	default:
		fmt.Fprintf(os.Stderr, "❌ Unknown shell %q. Use zsh, bash, fish or powershell.\n", shell)
		return 2
	}

	if failure != nil {
		fmt.Fprintln(os.Stderr, "❌", failure)
		return 1
	}
	if uninstall {
		fmt.Printf("✅ Tab completion is off for %s (removed from %s).\n", shell, strings.Join(done, ", "))
		return 0
	}
	fmt.Printf("✅ Tab completion is on for %s (set up in %s).\n", shell, strings.Join(done, ", "))
	for _, note := range notes {
		fmt.Println(note)
	}
	fmt.Println("Open a new terminal window, then type 'whatsapp-tui send ' and the first letters of a name, and press Tab.")
	fmt.Println("To turn it off again: whatsapp-tui completion uninstall")
	return 0
}

// currentShell guesses the user's shell.
func currentShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return "zsh"
	case "bash":
		return "bash"
	case "fish":
		return "fish"
	}
	return ""
}

// executablePath is the absolute, symlink-free path of this program, so the
// shell setup works from any folder.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Abs(exe)
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func zshDir() string {
	if dir := os.Getenv("ZDOTDIR"); dir != "" {
		return dir
	}
	return homeDir()
}

func tildePath(path string) string {
	home := homeDir()
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}

// shellBlock wraps setup lines in the markers used to find them again.
func shellBlock(shell, body string) string {
	return blockStart + "\n" +
		"# Tab completion for whatsapp-tui (" + shell + "). Remove with: whatsapp-tui completion uninstall\n" +
		body + "\n" + blockEnd + "\n"
}

// shQuote quotes a path for sh, bash and zsh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote quotes a path for PowerShell.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// updateBlock adds, replaces or removes the marked block in a config file,
// keeping everything else as it is.
func updateBlock(path, block string, remove bool) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		return fmt.Errorf("%s is saved as UTF-16, which this can't edit safely; add the line from 'whatsapp-tui completion --help' by hand", path)
	}
	text := removeBlock(string(data))
	if remove {
		if text == string(data) {
			return nil // nothing to remove
		}
	} else {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}
		text += block
	}
	return writeFileKeepingMode(path, []byte(text))
}

// removeBlock deletes the marked block (and one blank line before it).
func removeBlock(text string) string {
	start := strings.Index(text, blockStart)
	if start < 0 {
		return text
	}
	end := strings.Index(text[start:], blockEnd)
	if end < 0 {
		return text
	}
	end += start + len(blockEnd)
	if end < len(text) && text[end] == '\n' {
		end++
	}
	before := text[:start]
	if strings.HasSuffix(before, "\n\n") {
		before = before[:len(before)-1]
	}
	return before + text[end:]
}

// updateFishFile writes or removes our fish completion file. fish loads
// files from that folder by itself, so no config file needs changing.
func updateFishFile(path string, remove bool) error {
	if remove {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(data), fishHeader) {
			return fmt.Errorf("%s wasn't made by whatsapp-tui, so it was left alone", path)
		}
		return os.Remove(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileKeepingMode(path, []byte(fishCompletion))
}

// writeFileKeepingMode replaces a file atomically, keeping its permissions.
func writeFileKeepingMode(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".whatsapp-tui-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// powershellProfiles asks each installed PowerShell (Windows PowerShell and
// PowerShell 7) where its profile file is.
func powershellProfiles() []string {
	var profiles []string
	seen := map[string]bool{}
	for _, name := range []string{"powershell", "pwsh"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, "-NoProfile", "-NonInteractive", "-Command", "$PROFILE.CurrentUserCurrentHost")
		hideConsole(cmd)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		profile := strings.TrimSpace(string(out))
		if profile != "" && !seen[strings.ToLower(profile)] {
			seen[strings.ToLower(profile)] = true
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

// powershellBlocksProfiles reports whether PowerShell's execution policy
// stops profile scripts from running (the Windows default).
func powershellBlocksProfiles() bool {
	path, err := exec.LookPath("powershell")
	if err != nil {
		return false
	}
	cmd := exec.Command(path, "-NoProfile", "-NonInteractive", "-Command", "Get-ExecutionPolicy")
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(out)) {
	case "Restricted", "AllSigned":
		return true
	}
	return false
}

// useDataNextToProgram switches to the program's own folder when the
// current one has no session but that one does, so `whatsapp-tui send`
// works from anywhere without --dir.
func useDataNextToProgram() {
	if _, err := os.Stat("session.db"); err == nil {
		return
	}
	exe, err := executablePath()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "session.db")); err == nil {
		_ = os.Chdir(dir)
	}
}
