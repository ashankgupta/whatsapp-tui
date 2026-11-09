package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"

	bubbletea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// reactionEmojis are the quick reactions offered in the chat view.
var reactionEmojis = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// callBannerLines is how many lines the incoming-call banner occupies.
func (m Model) callBannerLines() int {
	if m.activeCall != nil {
		return 2
	}
	return 0
}

// listRows is how many two-line rows fit on a list screen.
func (m Model) listRows() int {
	// Header, spacer, section label and footer; the contacts view adds a
	// three-line search box and a spacer.
	chrome := 4
	if m.step != 3 {
		chrome = 8
	}
	rows := (m.terminalHeight - chrome - m.callBannerLines()) / 2
	if rows < 1 {
		rows = 1
	}
	return rows
}

// chatLineBudget is how many rendered lines the chat history can occupy.
func (m Model) chatLineBudget() int {
	inputLines := len(m.messageLines)
	if inputLines < 1 {
		inputLines = 1
	}
	if m.attaching {
		inputLines = 1
	}
	// Two-line header, spacers around the history, bordered input and footer.
	lines := m.terminalHeight - 7 - inputLines - m.callBannerLines()
	if lines < 3 {
		lines = 3
	}
	return lines
}

func (m *Model) adjustScroll() {
	if m.selected < m.scrollOffset {
		m.scrollOffset = m.selected
	}
	if m.selected >= m.scrollOffset+m.listRows() {
		m.scrollOffset = m.selected - m.listRows() + 1
	}
}

func (m *Model) adjustConvScroll() {
	if m.convSelected < m.convScroll {
		m.convScroll = m.convSelected
	}
	if m.convSelected >= m.convScroll+m.listRows() {
		m.convScroll = m.convSelected - m.listRows() + 1
	}
}

func (m *Model) adjustSelection() {
	if m.selected >= len(m.filteredContacts) && len(m.filteredContacts) > 0 {
		m.selected = len(m.filteredContacts) - 1
	} else if len(m.filteredContacts) == 0 {
		m.selected = 0
	}
	m.scrollOffset = 0
	m.adjustScroll()
}

func (m *Model) filterContacts() {
	if m.searchQuery == "" {
		m.filteredContacts = m.contacts
		return
	}
	query := strings.ToLower(m.searchQuery)
	filtered := []contacts.Contact{}
	for _, c := range m.contacts {
		if strings.Contains(strings.ToLower(c.Name), query) || strings.Contains(strings.ToLower(c.Number), query) {
			filtered = append(filtered, c)
		}
	}
	m.filteredContacts = filtered
}

// maxChatScroll is the index of the first message visible when the chat is
// scrolled to the bottom. It accounts for variable-height media bubbles.
func (m *Model) maxChatScroll() int {
	if len(m.chatMessages) == 0 {
		return 0
	}

	budget := m.chatLineBudget()
	used := 0
	start := len(m.chatMessages) - 1
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		h := m.messageHeight(i)
		if used > 0 && used+h > budget {
			return i + 1
		}
		used += h
		start = i
		if used >= budget {
			return start
		}
	}
	return start
}

// keepChatBottom restores the bottom scroll position after the chat height changes.
func (m *Model) keepChatBottom(wasBottom bool) {
	if wasBottom && m.step == 1 && !m.reacting {
		m.chatScroll = m.maxChatScroll()
	}
}

// messageHeight returns the rendered line count of a single message,
// including any date separator shown above it.
func (m Model) messageHeight(i int) int {
	block := m.messageBlock(i, false)
	return strings.Count(block, "\n") + 1
}

// chatName resolves a chat key (phone number or group JID) to a display name.
func (m Model) chatName(key string) string {
	if key == "" {
		return "Unknown"
	}
	for _, c := range m.contacts {
		if c.Number == key {
			return c.Name
		}
	}
	if strings.HasSuffix(key, "@g.us") {
		return "Group " + strings.SplitN(key, "@", 2)[0]
	}
	if strings.Contains(key, "@") {
		return strings.SplitN(key, "@", 2)[0]
	}
	return key
}

// openChat loads the conversation history and switches to the chat view.
func (m *Model) openChat(chatJID string) {
	if m.step == 3 {
		m.returnStep = 3
	} else {
		m.returnStep = 0
	}
	m.activeChat = chatJID
	m.chatMessages = nil
	m.chatScroll = 0
	m.statusMessage = ""
	m.reacting = false
	m.reactionTarget = 0
	m.reactionScroll = 0
	m.attaching = false
	m.attachInput = ""
	m.messageLines = []string{""}
	m.currentLine = 0

	if m.store != nil {
		messages, err := m.store.Messages(chatJID, 500)
		if err != nil {
			m.statusMessage = "Could not load history: " + err.Error()
		} else {
			m.chatMessages = messages
		}
	}

	m.chatScroll = m.maxChatScroll()
	m.step = 1
	delete(m.unread, chatJID)
}

// refreshConversations reloads the chat list from the history store.
func (m *Model) refreshConversations() {
	if m.store == nil {
		return
	}
	conversations, err := m.store.Conversations()
	if err != nil {
		return
	}
	m.conversations = conversations
	if m.convSelected >= len(m.conversations) {
		m.convSelected = len(m.conversations) - 1
	}
	if m.convSelected < 0 {
		m.convSelected = 0
	}
}

// sendMessageResult is delivered back to Update after a send attempt.
type sendMessageResult struct {
	err     error
	text    string
	msgID   string
	chatJID string
	time    time.Time
}

// recipientJID builds the destination JID for the active chat.
func (m Model) recipientJID() (waTypes.JID, error) {
	if strings.Contains(m.activeChat, "@") {
		return waTypes.ParseJID(m.activeChat)
	}
	return waTypes.NewJID(m.activeChat, waTypes.DefaultUserServer), nil
}

func (m Model) sendMessageCmd() bubbletea.Cmd {
	return func() bubbletea.Msg {
		text := strings.TrimSpace(strings.Join(m.messageLines, "\n"))
		if text == "" {
			return sendMessageResult{err: fmt.Errorf("message cannot be empty")}
		}
		if m.client == nil || m.activeChat == "" {
			return sendMessageResult{err: fmt.Errorf("no chat selected")}
		}

		recipient, err := m.recipientJID()
		if err != nil {
			return sendMessageResult{err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := m.client.SendMessage(ctx, recipient, &waE2E.Message{Conversation: &text})
		if err != nil {
			return sendMessageResult{err: err}
		}

		return sendMessageResult{
			text:    text,
			msgID:   string(resp.ID),
			chatJID: m.activeChat,
			time:    resp.Timestamp,
		}
	}
}

// ownUser returns our own phone number (the local part of our JID).
func (m Model) ownUser() string {
	if m.client != nil && m.client.Store != nil && m.client.Store.ID != nil {
		return m.client.Store.ID.User
	}
	return "me"
}

// hasMediaPath reports whether the open chat already has a local file for a message.
func (m Model) hasMediaPath(waID string) bool {
	for i := range m.chatMessages {
		if m.chatMessages[i].WaID == waID && m.chatMessages[i].MediaPath != "" {
			return true
		}
	}
	return false
}

// saveSelectedMedia saves the selected media message.
// If not already in selection mode, enters media selection mode like reactions.
func (m *Model) saveSelectedMedia() bubbletea.Cmd {
	if m.isSavingMedia {
		m.statusMessage = "Please wait for the current save to finish"
		return nil
	}
	if len(m.chatMessages) == 0 {
		m.statusMessage = "No media in this chat"
		return nil
	}

	// If already in media save selection mode
	if m.savingMedia {
		if m.saveTarget < 0 || m.saveTarget >= len(m.chatMessages) {
			m.savingMedia = false
			m.statusMessage = "No message selected"
			return nil
		}
		target := &m.chatMessages[m.saveTarget]
		if target.MediaType == "" {
			m.savingMedia = false
			m.statusMessage = "Selected message has no media"
			return nil
		}
		if !fileExists(target.MediaPath) && len(target.MediaRaw) == 0 {
			m.savingMedia = false
			m.statusMessage = "Media is no longer available"
			return nil
		}
		if m.mediaPending[target.WaID] {
			m.savingMedia = false
			m.statusMessage = "Image is still downloading"
			return nil
		}
		m.savingMedia = false
		m.isSavingMedia = true
		m.statusMessage = "Saving media..."
		return m.saveMediaCmd(*target)
	}

	// Enter selection mode
	m.savingMedia = true
	m.saveTarget = len(m.chatMessages) - 1
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		if m.chatMessages[i].MediaType != "" {
			m.saveTarget = i
			break
		}
	}
	m.saveScroll = m.chatScroll
	m.statusMessage = ""
	return nil
}

// applyReaction updates the in-memory reactions of the open chat.
func (m *Model) applyReaction(chatJID, targetID, sender, emoji string) {
	if m.step != 1 || m.activeChat != chatJID {
		return
	}
	for i := range m.chatMessages {
		if m.chatMessages[i].WaID != targetID {
			continue
		}
		if m.chatMessages[i].Reactions == nil {
			m.chatMessages[i].Reactions = make(map[string]string)
		}
		if emoji == "" {
			delete(m.chatMessages[i].Reactions, sender)
		} else {
			m.chatMessages[i].Reactions[sender] = emoji
		}
		return
	}
}

// reactionTargetPreview describes the message currently selected for reacting.
func (m Model) reactionTargetPreview() string {
	if m.reactionTarget < 0 || m.reactionTarget >= len(m.chatMessages) {
		return "no message"
	}
	msg := m.chatMessages[m.reactionTarget]
	who := "You"
	if !msg.FromMe {
		who = m.senderName(msg)
	}
	text := strings.ReplaceAll(msg.Text, "\n", " ")
	return who + ": " + truncate(text, 28)
}

// reactionResult is delivered back to Update after a reaction attempt.
type reactionResult struct {
	err      error
	chatJID  string
	targetID string
	sender   string
	emoji    string
}

func (m Model) sendReactionCmd(target history.Message, emoji string) bubbletea.Cmd {
	return func() bubbletea.Msg {
		if m.client == nil || m.activeChat == "" {
			return reactionResult{err: fmt.Errorf("no chat selected")}
		}
		if target.WaID == "" {
			return reactionResult{err: fmt.Errorf("this message has no WhatsApp ID")}
		}

		recipient, err := m.recipientJID()
		if err != nil {
			return reactionResult{err: err}
		}

		key := &waCommon.MessageKey{
			RemoteJID: proto.String(recipient.String()),
			FromMe:    proto.Bool(target.FromMe),
			ID:        proto.String(target.WaID),
		}
		if strings.HasSuffix(m.activeChat, "@g.us") && !target.FromMe && target.SenderJID != "" {
			participant := waTypes.NewJID(target.SenderJID, waTypes.DefaultUserServer)
			if strings.Contains(target.SenderJID, "@") {
				participant, _ = waTypes.ParseJID(target.SenderJID)
			}
			key.Participant = proto.String(participant.String())
		}

		reaction := &waE2E.ReactionMessage{
			Key:               key,
			Text:              proto.String(emoji),
			GroupingKey:       proto.String(target.WaID),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if _, err := m.client.SendMessage(ctx, recipient, &waE2E.Message{ReactionMessage: reaction}); err != nil {
			return reactionResult{err: err}
		}

		return reactionResult{
			chatJID:  m.activeChat,
			targetID: target.WaID,
			sender:   m.ownUser(),
			emoji:    emoji,
		}
	}
}

// whatsappNumber returns the phone number for the currently selected chat.
func (m Model) whatsappNumber() (string, error) {
	var key string
	switch m.step {
	case 0:
		if len(m.filteredContacts) == 0 || m.selected >= len(m.filteredContacts) {
			return "", fmt.Errorf("no contact selected")
		}
		key = m.filteredContacts[m.selected].Number
	case 1:
		key = m.activeChat
	case 3:
		if len(m.conversations) == 0 || m.convSelected >= len(m.conversations) {
			return "", fmt.Errorf("no chat selected")
		}
		key = m.conversations[m.convSelected].ChatJID
	default:
		return "", fmt.Errorf("no chat selected")
	}
	if key == "" {
		return "", fmt.Errorf("no chat selected")
	}
	if strings.Contains(key, "@") {
		return "", fmt.Errorf("WhatsApp deep links only work for direct chats")
	}
	return key, nil
}

type openWhatsAppResult struct {
	err    error
	target string
}

// openWhatsAppCmd opens the contact's chat in the WhatsApp app or web client.
func openWhatsAppCmd(number string) bubbletea.Cmd {
	return func() bubbletea.Msg {
		number = strings.TrimPrefix(number, "+")
		targets := []string{
			"whatsapp://send?phone=" + number,
			"https://wa.me/" + number,
		}
		var lastErr error
		for _, target := range targets {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := exec.CommandContext(ctx, "xdg-open", target).Run()
			cancel()
			if err == nil {
				return openWhatsAppResult{target: target}
			}
			lastErr = err
		}
		return openWhatsAppResult{err: lastErr}
	}
}

type rejectCallResult struct {
	err error
}

func (m Model) rejectCallCmd(call whatsapp.CallEvent) bubbletea.Cmd {
	return func() bubbletea.Msg {
		if m.client == nil {
			return rejectCallResult{err: fmt.Errorf("not connected")}
		}
		from := call.From
		if from == "" {
			from = call.GroupJID
		}
		jid, err := waTypes.ParseJID(from)
		if err != nil {
			return rejectCallResult{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := m.client.RejectCall(ctx, jid, call.CallID); err != nil {
			return rejectCallResult{err: err}
		}
		return rejectCallResult{}
	}
}

// incomingMessageMsg wraps a received WhatsApp message for the Bubble Tea update loop.
type incomingMessageMsg whatsapp.IncomingMessage

// incomingCallMsg wraps a received WhatsApp call event for the Bubble Tea update loop.
type incomingCallMsg whatsapp.CallEvent

// waitForIncoming blocks until the next incoming message arrives.
func (m Model) waitForIncoming() bubbletea.Cmd {
	return func() bubbletea.Msg {
		msg, ok := <-m.incoming
		if !ok {
			return nil
		}
		return incomingMessageMsg(msg)
	}
}

// waitForCall blocks until the next incoming call event arrives.
func (m Model) waitForCall() bubbletea.Cmd {
	if m.incomingCalls == nil {
		return nil
	}
	return func() bubbletea.Msg {
		event, ok := <-m.incomingCalls
		if !ok {
			return nil
		}
		return incomingCallMsg(event)
	}
}
