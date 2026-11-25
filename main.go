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

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("whatsapp-tui %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	// Load contacts (contacts.json is created on first run)
	const contactsFile = "contacts.json"
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
	client := whatsapp.Connect("file:session.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")

	// Listen for incoming messages, calls, archive changes and name updates
	incoming := whatsapp.Listen(client)
	incomingCalls := whatsapp.ListenCalls(client)
	archiveEvents := whatsapp.ListenArchive(client)
	nameEvents := whatsapp.ListenNames(client)

	// Check for Sixel image support before the TUI takes over the terminal
	tui.DetectGraphics()

	// Start the TUI
	p := bubbletea.NewProgram(tui.NewModel(contactsFile, contactsList, client, incoming, incomingCalls, archiveEvents, nameEvents, store), bubbletea.WithAltScreen())

	if err := p.Start(); err != nil {
		fmt.Println("💥 App crashed:", err)
		os.Exit(1)
	}
}
