package tui

import (
	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"

	"go.mau.fi/whatsmeow"
)

// NewModel creates a new TUI model with the given contacts (and the file they
// are saved to), WhatsApp client, event streams and persistent history store.
func NewModel(contactsPath string, contacts []contacts.Contact, client *whatsmeow.Client, incoming <-chan whatsapp.IncomingMessage, incomingCalls <-chan whatsapp.CallEvent, archiveEvents <-chan whatsapp.ArchiveEvent, nameEvents <-chan whatsapp.NameUpdate, store *history.Store) *Model {
	m := &Model{
		contactsPath:     contactsPath,
		peopleNames:      make(map[string]string),
		groupNames:       make(map[string]string),
		nameEvents:       nameEvents,
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
		archiveEvents:    archiveEvents,
		archived:         make(map[string]bool),
		unread:           make(map[string]int),
		imagePreviews:    make(map[string]string),
		imagePreviewErrs: make(map[string]string),
		sixels:           make(map[int]*sixelImage),
		sixelNextID:      new(int),
		mediaPending:     make(map[string]bool),
		mediaErrors:      make(map[string]string),
	}

	m.refreshConversations()

	return m
}
