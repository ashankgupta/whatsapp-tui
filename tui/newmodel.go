package tui

import (
	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"

	"go.mau.fi/whatsmeow"
)

// NewModel creates a new TUI model with the given contacts, WhatsApp client and
// persistent history store.
func NewModel(contacts []contacts.Contact, client *whatsmeow.Client, incoming <-chan whatsapp.IncomingMessage, incomingCalls <-chan whatsapp.CallEvent, store *history.Store) *Model {
	m := &Model{
		contacts:         contacts,
		filteredContacts: contacts,
		selected:         0,
		messageLines:     []string{""},
		searchQuery:      "",
		step:             0,
		client:           client,
		store:            store,
		statusMessage:    "",
		isSending:        false,
		windowHeight:     10,
		terminalHeight:   24,
		scrollOffset:     0,
		terminalWidth:    80,
		currentLine:      0,
		incoming:         incoming,
		incomingCalls:    incomingCalls,
		unread:           make(map[string]int),
		imagePreviews:    make(map[string]string),
		imagePreviewErrs: make(map[string]string),
		mediaPending:     make(map[string]bool),
		mediaErrors:      make(map[string]string),
	}

	if store != nil {
		if conversations, err := store.Conversations(); err == nil {
			m.conversations = conversations
		}
	}

	return m
}
