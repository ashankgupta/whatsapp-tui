package main

import (
	"fmt"
	"os"

	bubbletea "github.com/charmbracelet/bubbletea"
	_ "github.com/mattn/go-sqlite3"

	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/tui"
	"whatsapp-tui/whatsapp"
)

func main() {
	// Load contacts
	contactsList := contacts.LoadContacts("contacts.json")

	// Open persistent chat history
	store, err := history.Open("history.db")
	if err != nil {
		fmt.Println("💥 Could not open chat history:", err)
		os.Exit(1)
	}
	defer store.Close()

	// Connect to WhatsApp (session is persisted in session.db)
	client := whatsapp.Connect("file:session.db?_foreign_keys=on")

	// Listen for incoming messages and calls
	incoming := whatsapp.Listen(client)
	incomingCalls := whatsapp.ListenCalls(client)

	// Start the TUI
	p := bubbletea.NewProgram(tui.NewModel(contactsList, client, incoming, incomingCalls, store), bubbletea.WithAltScreen())

	if err := p.Start(); err != nil {
		fmt.Println("💥 App crashed:", err)
		os.Exit(1)
	}
}
