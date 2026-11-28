package main

// Shell completion: `whatsapp-tui completion zsh|bash|fish` prints a script,
// and the script calls the hidden `whatsapp-tui __names <prefix>` to suggest
// recipients for `send`. Suggestions come only from local data (contacts.json,
// the address book in session.db and cached group names in history.db), so
// they appear instantly and never touch the network.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/tui"
	"whatsapp-tui/whatsapp"
)

const completionUsage = `Tab completion: after 'whatsapp-tui send', Tab suggests contacts and groups.

Turn it on (or off) for your shell in one step:
  whatsapp-tui completion install
  whatsapp-tui completion uninstall

It finds your shell by itself; name it to pick another, e.g.
'whatsapp-tui completion install bash'. Linux packages (.deb, .rpm, Arch)
already set it up for bash, zsh and fish.

Or print the script and set it up by hand:
  zsh:   echo 'source <(whatsapp-tui completion zsh)' >> ~/.zshrc
  bash:  echo 'source <(whatsapp-tui completion bash)' >> ~/.bashrc
  fish:  whatsapp-tui completion fish > ~/.config/fish/completions/whatsapp-tui.fish
  PowerShell (Windows):
         Add-Content $PROFILE 'whatsapp-tui completion powershell | Out-String | Invoke-Expression'

Open a new terminal afterwards. Suggestions use the data folder you run the
command in, the program's own folder, or the one given with --dir.
`

// runCompletion prints the script for a shell.
func runCompletion(args []string) int {
	if len(args) > 0 && (args[0] == "install" || args[0] == "uninstall") {
		return runCompletionInstall(args[1:], args[0] == "uninstall")
	}
	if len(args) != 1 {
		fmt.Print(completionUsage)
		if len(args) == 0 {
			return 0
		}
		return 2
	}
	switch args[0] {
	case "zsh":
		os.Stdout.WriteString(zshCompletion)
	case "bash":
		os.Stdout.WriteString(bashCompletion)
	case "fish":
		os.Stdout.WriteString(fishCompletion)
	case "powershell", "pwsh":
		os.Stdout.WriteString(powershellCompletion)
	case "-h", "--help", "help":
		fmt.Print(completionUsage)
	default:
		fmt.Fprintf(os.Stderr, "whatsapp-tui completion: unknown shell %q (zsh, bash, fish or powershell)\n", args[0])
		return 2
	}
	return 0
}

// runNames prints "word<TAB>description" lines for recipients matching the
// prefix. It never fails loudly: completion must not print errors.
func runNames(args []string) int {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}

	// Don't create contacts.json in some random folder while completing.
	var saved []contacts.Contact
	if _, err := os.Stat(contactsFile); err == nil {
		saved, _ = contacts.LoadContacts(contactsFile)
	}

	people := map[string]string{}
	if _, err := os.Stat("session.db"); err == nil {
		if client := whatsapp.OpenOffline(sessionDSN); client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			people = whatsapp.PeopleNames(ctx, client)
			cancel()
		}
	}

	groups := map[string]string{}
	if _, err := os.Stat("history.db"); err == nil {
		if store, err := history.Open("history.db"); err == nil {
			groups, _ = store.ChatNames()
			store.Close()
		}
	}

	for _, s := range tui.Suggestions(saved, people, groups, prefix, 200) {
		word := strings.NewReplacer("\t", " ", "\n", " ").Replace(s.Word)
		fmt.Printf("%s\t%s\n", word, s.Description)
	}
	return 0
}

const zshCompletion = `#compdef whatsapp-tui
# zsh completion for whatsapp-tui. Load with:
#   source <(whatsapp-tui completion zsh)

_whatsapp_tui() {
  local -a dir
  local cmd="" i positional=0 skip=0
  for (( i = 2; i < CURRENT; i++ )); do
    if (( skip )); then skip=0; continue; fi
    case ${words[i]} in
      --dir|-C) [[ -z $cmd ]] && dir=(--dir "${(Q)words[i+1]}"); skip=1 ;;
      -f|--file) skip=1 ;;
      -*) ;;
      *)
        if [[ -z $cmd ]]; then cmd=${words[i]}
        else (( positional++ )); fi ;;
    esac
  done

  case ${words[CURRENT-1]} in
    --dir|-C) _files -/; return ;;
    -f|--file) _files; return ;;
  esac

  if [[ -z $cmd ]]; then
    if [[ $PREFIX == -* ]]; then
      compadd -- --dir --help --version
    else
      local -a commands=(
        'send:send a message or file'
        'completion:print a shell completion script'
        'help:show help'
      )
      _describe command commands
    fi
    return
  fi

  case $cmd in
    send)
      if [[ $PREFIX == -* ]]; then
        compadd -- --file --help
      elif (( positional == 0 )); then
        local -a words_ displays
        local line word desc
        for line in "${(@f)$(${~${(Q)words[1]}} $dir __names "${(Q)PREFIX}" 2>/dev/null)}"; do
          [[ -z $line ]] && continue
          word=${line%%$'\t'*}
          desc=${line#*$'\t'}
          words_+=("$word")
          displays+=("$word  -- $desc")
        done
        (( ${#words_} )) || return
        # Names that start with what was typed complete normally (any case).
        # Otherwise ("gu" finds "Ashank Gupta"), cycle through the full names
        # with Tab instead of inserting the bits they happen to share.
        local typed=${(L)${(Q)PREFIX}} w all_prefix=1
        for w in "${words_[@]}"; do
          [[ ${(L)w} == "$typed"* ]] || { all_prefix=0; break; }
        done
        if (( all_prefix )); then
          compadd -M 'm:{[:lower:][:upper:]}={[:upper:][:lower:]}' -l -d displays -- "${words_[@]}"
        else
          compadd -U -l -d displays -- "${words_[@]}"
          compstate[insert]=menu
          compstate[list]='list force'
        fi
      fi ;;
    completion)
      if (( positional == 0 )); then compadd -- install uninstall zsh bash fish powershell
      elif (( positional == 1 )) && [[ ${words[CURRENT-1]} == (install|uninstall) ]]; then compadd -- zsh bash fish powershell; fi ;;
  esac
}

if [[ ${zsh_eval_context[-1]} == loadautofunc ]]; then
  _whatsapp_tui "$@"
else
  compdef _whatsapp_tui whatsapp-tui
fi
`

const bashCompletion = `# bash completion for whatsapp-tui. Load with:
#   source <(whatsapp-tui completion bash)

_whatsapp_tui() {
  local cur=${COMP_WORDS[COMP_CWORD]} prev=${COMP_WORDS[COMP_CWORD-1]}
  local cmd="" positional=0 skip=0 i
  local -a dir=()
  for (( i = 1; i < COMP_CWORD; i++ )); do
    if (( skip )); then skip=0; continue; fi
    case ${COMP_WORDS[i]} in
      --dir|-C) [[ -z $cmd ]] && dir=(--dir "${COMP_WORDS[i+1]}"); skip=1 ;;
      -f|--file) skip=1 ;;
      -*) ;;
      *) if [[ -z $cmd ]]; then cmd=${COMP_WORDS[i]}; else (( positional++ )); fi ;;
    esac
  done

  case $prev in
    --dir|-C) COMPREPLY=($(compgen -d -- "$cur")); return ;;
    -f|--file) COMPREPLY=($(compgen -f -- "$cur")); return ;;
  esac

  if [[ -z $cmd ]]; then
    COMPREPLY=($(compgen -W "send completion help --dir --help --version" -- "$cur"))
    return
  fi

  case $cmd in
    send)
      if [[ $cur == -* ]]; then
        COMPREPLY=($(compgen -W "--file --help" -- "$cur"))
      elif (( positional == 0 )); then
        local prefix=${cur#[\"\']} line
        COMPREPLY=()
        local word restore
        restore=$(shopt -p nocasematch)
        shopt -s nocasematch
        while IFS= read -r line; do
          word=${line%%$'\t'*}
          # Readline replaces the word with what all matches share, so offer
          # only names that start with what was typed (ignoring case).
          [[ -n $word && $word == "$prefix"* ]] && COMPREPLY+=("$(printf '%q' "$word")")
        done < <("${COMP_WORDS[0]}" "${dir[@]}" __names "$prefix" 2>/dev/null)
        $restore
      fi ;;
    completion)
      if (( positional == 0 )); then COMPREPLY=($(compgen -W "install uninstall zsh bash fish powershell" -- "$cur"))
      elif [[ $prev == install || $prev == uninstall ]]; then COMPREPLY=($(compgen -W "zsh bash fish powershell" -- "$cur")); fi ;;
  esac
}

complete -o default -F _whatsapp_tui whatsapp-tui
`

const fishCompletion = `# fish completion for whatsapp-tui. Install with:
#   whatsapp-tui completion fish > ~/.config/fish/completions/whatsapp-tui.fish

function __whatsapp_tui_send_needs_to
    set -l tokens (commandline -opc)
    set -l seen_send 0
    set -l skip 0
    for t in $tokens[2..-1]
        if test $skip = 1
            set skip 0
            continue
        end
        switch $t
            case --dir -C -f --file
                set skip 1
            case '-*'
            case send
                if test $seen_send = 0
                    set seen_send 1
                    continue
                end
                test $seen_send = 1; and return 1
            case '*'
                test $seen_send = 1; and return 1
        end
    end
    test $seen_send = 1
end

function __whatsapp_tui_names
    set -l tokens (commandline -opc)
    set -l dir
    set -l i (contains -i -- --dir $tokens; or contains -i -- -C $tokens)
    if test -n "$i"
        set dir --dir $tokens[(math $i + 1)]
    end
    $tokens[1] $dir __names (commandline -ct) 2>/dev/null
end

complete -c whatsapp-tui -f
complete -c whatsapp-tui -n __fish_use_subcommand -a send -d 'Send a message or file'
complete -c whatsapp-tui -n __fish_use_subcommand -a completion -d 'Print a shell completion script'
complete -c whatsapp-tui -n __fish_use_subcommand -a help -d 'Show help'
complete -c whatsapp-tui -n __fish_use_subcommand -l dir -s C -r -a '(__fish_complete_directories)' -d 'Data folder'
complete -c whatsapp-tui -n __whatsapp_tui_send_needs_to -a '(__whatsapp_tui_names)'
complete -c whatsapp-tui -n '__fish_seen_subcommand_from send' -s f -l file -r -F -d 'Send a file'
complete -c whatsapp-tui -n '__fish_seen_subcommand_from completion' -a 'install uninstall zsh bash fish powershell'
`

const powershellCompletion = `# PowerShell completion for whatsapp-tui. Load it in your profile with:
#   Add-Content $PROFILE 'whatsapp-tui completion powershell | Out-String | Invoke-Expression'

Register-ArgumentCompleter -Native -CommandName @(
    'whatsapp-tui', 'whatsapp-tui.exe',
    '.\whatsapp-tui', '.\whatsapp-tui.exe', './whatsapp-tui', './whatsapp-tui.exe'
) -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $elements = $commandAst.CommandElements
    $exe = $elements[0].Extent.Text.Trim('"', "'")

    # Words before the one being completed.
    $before = @()
    foreach ($e in ($elements | Select-Object -Skip 1)) {
        if ($e.Extent.EndOffset -ge $cursorPosition -and $wordToComplete -ne '') { break }
        if ($e.Extent.StartOffset -ge $cursorPosition) { break }
        $before += $e.Extent.Text.Trim('"', "'")
    }

    $cmd = ''; $positional = 0; $skip = $false; $dir = @()
    for ($i = 0; $i -lt $before.Count; $i++) {
        if ($skip) { $skip = $false; continue }
        $t = $before[$i]
        if ($t -eq '--dir' -or $t -eq '-C') {
            if ($cmd -eq '' -and $i + 1 -lt $before.Count) { $dir = @('--dir', $before[$i + 1]) }
            $skip = $true
        } elseif ($t -eq '-f' -or $t -eq '--file') {
            $skip = $true
        } elseif ($t.StartsWith('-')) {
        } elseif ($cmd -eq '') {
            $cmd = $t
        } else {
            $positional++
        }
    }

    $prev = if ($before.Count -gt 0) { $before[-1] } else { '' }
    # Folders and files: return nothing so PowerShell completes paths itself.
    if ($prev -in '--dir', '-C', '-f', '--file') { return }

    $prefix = $wordToComplete.Trim('"', "'")
    $choices = @()
    $fixed = $null

    if ($cmd -eq '') {
        $fixed = 'send', 'completion', 'help', '--dir', '--help', '--version'
    } elseif ($cmd -eq 'completion' -and $positional -eq 0) {
        $fixed = 'install', 'uninstall', 'powershell', 'bash', 'zsh', 'fish'
    } elseif ($cmd -eq 'send' -and $prefix.StartsWith('-')) {
        $fixed = '--file', '--help'
    } elseif ($cmd -eq 'send' -and $positional -eq 0) {
        # The names are UTF-8 (emoji, other scripts); read them as such.
        $encoding = [Console]::OutputEncoding
        try {
            [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
            $lines = & $exe @dir __names $prefix 2>$null
        } finally {
            [Console]::OutputEncoding = $encoding
        }
        foreach ($line in @($lines)) {
            if (-not $line) { continue }
            $parts = $line -split "\t", 2
            $tip = if ($parts.Count -gt 1 -and $parts[1]) { $parts[1] } else { $parts[0] }
            $choices += [pscustomobject]@{ Word = $parts[0]; Tip = $tip }
        }
    } else {
        return
    }
    if ($fixed) {
        foreach ($f in $fixed) {
            if ($f -like "$prefix*") { $choices += [pscustomobject]@{ Word = $f; Tip = $f } }
        }
    }

    foreach ($c in $choices) {
        $text = $c.Word
        # Quote names with spaces or characters PowerShell treats specially.
        if ($text -match '[\s''"(){};,&|<>@$#\x60]') {
            $text = "'" + $text.Replace("'", "''") + "'"
        }
        [System.Management.Automation.CompletionResult]::new($text, $c.Word, 'ParameterValue', $c.Tip)
    }
}
`
