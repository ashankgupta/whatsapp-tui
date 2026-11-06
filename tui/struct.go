package tui

import (
	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"

	"go.mau.fi/whatsmeow"
)

// Model represents the state of the TUI application.
type Model struct {
	contacts         []contacts.Contact
	filteredContacts []contacts.Contact
	selected         int
	messageLines     []string
	searchQuery      string
	step             int
	client           *whatsmeow.Client
	store            *history.Store
	statusMessage    string
	isSending        bool
	windowHeight     int
	terminalHeight   int
	scrollOffset     int
	terminalWidth    int
	currentLine      int
	cursorPos        int
	incoming         <-chan whatsapp.IncomingMessage
	incomingCalls    <-chan whatsapp.CallEvent
	activeCall       *whatsapp.CallEvent
	activeChat       string
	chatMessages     []history.Message
	chatScroll       int
	conversations    []history.Conversation
	convSelected     int
	convScroll       int
	unread           map[string]int
	returnStep       int
	reacting         bool
	reactionTarget   int
	reactionScroll   int
	attaching        bool
	attachInput      string
	isUploading      bool
	isSavingMedia    bool
	savingMedia      bool
	saveTarget       int
	saveScroll       int
	imagePreviews    map[string]string
	imagePreviewErrs map[string]string
	mediaPending     map[string]bool
	mediaErrors      map[string]string
}
