package main

import (
	"fmt"
	"os"

	bubbletea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"

	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/tui"
	"whatsapp-tui/whatsapp"
)

// Set at build time via -ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// contactsFile holds the names the user saved; created on first run.
const contactsFile = "contacts.json"

func main() {
	args := os.Args[1:]

	// --dir <folder> runs with the data in another folder.
	dirGiven := false
	if len(args) >= 1 && (args[0] == "--dir" || args[0] == "-C") {
		dirGiven = true
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "whatsapp-tui: --dir needs a folder")
			os.Exit(2)
		}
		if err := os.Chdir(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "whatsapp-tui:", err)
			os.Exit(1)
		}
		args = args[2:]
	}

	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Printf("whatsapp-tui %s (commit %s, built %s)\n", version, commit, date)
			return
		case "--help", "-h", "help":
			fmt.Print(mainUsage)
			return
		case "send":
			if !dirGiven {
				useDataNextToProgram()
			}
			os.Exit(runSend(args[1:]))
		case "completion":
			os.Exit(runCompletion(args[1:]))
		case "__names":
			if !dirGiven {
				useDataNextToProgram()
			}
			os.Exit(runNames(args[1:]))
		default:
			fmt.Fprintf(os.Stderr, "whatsapp-tui: unknown command %q\n\n%s", args[0], mainUsage)
			os.Exit(2)
		}
	}

	runApp()
}

// runApp opens the terminal UI.
func runApp() {
	contactsList, err := contacts.LoadContacts(contactsFile)
	if err != nil {
		fmt.Println("💥 Could not load contacts:", err)
		os.Exit(1)
	}

	// Open persistent chat history
	store, err := history.Open("history.db")
	if err != nil {
		fmt.Println("💥 Could not open chat history:", err)
		os.Exit(1)
	}
	defer store.Close()

	// Connect to WhatsApp (session is persisted in session.db)
	client := whatsapp.Connect(sessionDSN)

	// Listen for incoming messages, calls, archive changes and name updates
	incoming := whatsapp.Listen(client)
	incomingCalls := whatsapp.ListenCalls(client)
	archiveEvents := whatsapp.ListenArchive(client)
	nameEvents := whatsapp.ListenNames(client)

	// Check for Sixel image support before the TUI takes over the terminal
	tui.DetectGraphics()

	p := bubbletea.NewProgram(tui.NewModel(contactsFile, contactsList, client, incoming, incomingCalls, archiveEvents, nameEvents, store), bubbletea.WithAltScreen())

	// Let `whatsapp-tui send` use this connection while the app is open.
	stopControl := serveControl(client, store, p)
	defer stopControl()

	if _, err := p.Run(); err != nil {
		stopControl()
		fmt.Println("💥 App crashed:", err)
		os.Exit(1)
	}
}
