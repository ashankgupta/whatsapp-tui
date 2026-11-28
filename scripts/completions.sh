#!/bin/sh
# Generates shell completion files for the release archives and Linux
# packages (run by GoReleaser before building).
set -e
rm -rf completions
mkdir completions
for shell in bash zsh fish powershell; do
	go run . completion "$shell" > "completions/whatsapp-tui.$shell"
done
mv completions/whatsapp-tui.powershell completions/whatsapp-tui.ps1
