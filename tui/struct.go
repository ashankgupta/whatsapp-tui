package tui

import (
	"time"

	"whatsapp-tui/audio"
	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"

	"go.mau.fi/whatsmeow"
)

// Model represents the state of the TUI application.
type Model struct {
	contacts           []contacts.Contact
	contactsPath       string
	form               *contactForm      // open contact dialog, if any
	peopleNames        map[string]string // names WhatsApp knows for people
	groupNames         map[string]string // group and channel titles
	nameEvents         <-chan whatsapp.NameUpdate
	namesReloadPending bool
	filteredContacts   []contacts.Contact
	selected           int
	messageLines       []string
	searchQuery        string
	step               int
	client             *whatsmeow.Client
	store              *history.Store
	statusMessage      string
	isSending          bool
	windowHeight       int
	terminalHeight     int
	scrollOffset       int
	terminalWidth      int
	currentLine        int
	cursorPos          int
	incoming           <-chan whatsapp.IncomingMessage
	incomingCalls      <-chan whatsapp.CallEvent
	archiveEvents      <-chan whatsapp.ArchiveEvent
	archived           map[string]bool // cached archive state per chat
	showArchived       bool
	addRowSelected     bool   // "New contact" row at the top of the contacts list
	chatSearching      bool   // typing into the recent chats search box
	chatSearch         string // recent chats search query
	archivedCount      int
	isArchiving        bool
	activeCall         *whatsapp.CallEvent
	activeChat         string
	chatMessages       []history.Message
	chatScroll         int
	conversations      []history.Conversation
	chatList           []history.Conversation // conversations shown in the recent chats view
	convSelected       int
	convScroll         int
	unread             map[string]int
	returnStep         int
	reacting           bool
	reactionTarget     int
	reactionScroll     int
	attaching          bool
	attachInput        string
	isUploading        bool
	isSavingMedia      bool
	savingMedia        bool
	saveTarget         int
	saveScroll         int
	imagePreviews      map[string]string
	imagePreviewErrs   map[string]string
	sixels             map[int]*sixelImage
	sixelNextID        *int
	mediaPending       map[string]bool
	mediaErrors        map[string]string
	recorder           *audio.Recorder // voice message being recorded
	player             *audio.Player   // voice message being played
	playingID          string          // WhatsApp ID of the message being played
	playingLength      time.Duration
	choosingAudio      bool // picking a voice message to play
	audioTarget        int
	audioScroll        int
	statusSeq          int // bumped on every notification change
}
