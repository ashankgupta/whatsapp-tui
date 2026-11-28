package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
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

// selecting reports whether a message picker (react or message actions) is
// open. The chat is then pinned to the picked message, not the bottom.
func (m Model) selecting() bool {
	return m.reacting || m.acting
}

// composerLines is the height of the "Replying to…" / "Editing" line above the input.
func (m Model) composerLines() int {
	if m.replyTo != nil || m.editing != nil {
		return 1
	}
	return 0
}

// listRows is how many two-line rows fit on a list screen.
func (m Model) listRows() int {
	// Header, spacer, three-line search box, spacer, section label and footer.
	chrome := 8
	if m.step == 0 {
		chrome += 2 // the "New contact" row
	}
	if m.form != nil {
		chrome += m.formLines() + 2
	}
	rows := (m.terminalHeight - chrome - m.callBannerLines() - m.statusLines()) / 2
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
	if m.form != nil {
		inputLines = m.formLines()
	}
	// Two-line header, spacers around the history, bordered input and footer.
	lines := m.terminalHeight - 7 - inputLines - m.callBannerLines() - m.statusLines() - m.composerLines()
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

// addContactSelected reports whether the "New contact" row is highlighted.
// It is the only row when there are no contacts to show.
func (m Model) addContactSelected() bool {
	return m.addRowSelected || len(m.filteredContacts) == 0
}

func (m *Model) filterContacts() {
	m.addRowSelected = false
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
// scrolled to the bottom. That message may be only partly visible, clipped at
// the top edge like in mobile chat apps, so the view is always filled.
func (m *Model) maxChatScroll() int {
	budget := m.chatLineBudget()
	used := 0
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		used += m.messageHeight(i)
		if used >= budget {
			return i
		}
	}
	return 0
}

// scrollToMessage scrolls the chat just enough to show message i, keeping
// the messages around it on screen.
func (m *Model) scrollToMessage(i int) {
	bottom := m.maxChatScroll()
	if i >= bottom {
		// The message fits in the bottom page: show the newest messages.
		m.chatScroll = bottom
		return
	}
	if i < m.chatScroll {
		m.chatScroll = i
		return
	}
	// Already visible if it starts within the current page.
	budget, used := m.chatLineBudget(), 0
	for j := m.chatScroll; j < i; j++ {
		used += m.messageHeight(j)
	}
	if used+m.messageHeight(i) > budget {
		m.chatScroll = i
	}
}

// keepChatBottom restores the bottom scroll position after the chat height changes.
func (m *Model) keepChatBottom(wasBottom bool) {
	if wasBottom && m.step == 1 && !m.selecting() {
		m.chatScroll = m.maxChatScroll()
	}
}

// messageHeight returns the rendered line count of a single message,
// including any date separator shown above it.
func (m Model) messageHeight(i int) int {
	block := m.messageBlock(i, false)
	return strings.Count(block, "\n") + 1
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
	m.acting = false
	m.confirmDelete = false
	m.replyTo, m.editing = nil, nil
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

	m.chatList = m.chatList[:0]
	m.archivedCount = 0
	query := strings.ToLower(strings.TrimSpace(m.chatSearch))
	for _, c := range conversations {
		archived := m.isArchived(c.ChatJID)
		if archived {
			m.archivedCount++
		}
		switch {
		case query != "":
			// Like WhatsApp, search finds archived chats too.
			if m.chatMatches(c.ChatJID, query) {
				m.chatList = append(m.chatList, c)
			}
		case archived == m.showArchived:
			m.chatList = append(m.chatList, c)
		}
	}
	if m.convSelected >= len(m.chatList) {
		m.convSelected = len(m.chatList) - 1
	}
	if m.convSelected < 0 {
		m.convSelected = 0
	}
}

// chatMatches reports whether a chat's name or number contains the query.
func (m Model) chatMatches(chatJID, query string) bool {
	if strings.Contains(strings.ToLower(m.chatName(chatJID)), query) {
		return true
	}
	digits := strings.NewReplacer("+", "", " ", "", "-", "").Replace(query)
	return digits != "" && strings.Contains(strings.SplitN(chatJID, "@", 2)[0], digits)
}

// setChatSearch updates the recent chats search and jumps to the top result.
func (m *Model) setChatSearch(query string) {
	m.chatSearch = query
	m.convSelected = 0
	m.convScroll = 0
	m.refreshConversations()
}

// stopChatSearch leaves search mode and shows the full list again.
func (m *Model) stopChatSearch() {
	m.chatSearching = false
	if m.chatSearch != "" {
		m.setChatSearch("")
	}
}

// handleChatSearchKey handles typing while the recent chats search is open.
// Letters go into the search instead of triggering shortcuts like a or q.
func (m *Model) handleChatSearchKey(msg bubbletea.KeyMsg) (bubbletea.Cmd, bool) {
	if !m.chatSearching {
		return nil, false
	}
	switch msg.Type {
	case bubbletea.KeyEsc:
		m.stopChatSearch()
	case bubbletea.KeyEnter:
		if len(m.chatList) == 0 || m.convSelected >= len(m.chatList) {
			return nil, true
		}
		chat := m.chatList[m.convSelected].ChatJID
		m.stopChatSearch()
		m.openChat(chat)
	case bubbletea.KeyBackspace:
		if r := []rune(m.chatSearch); len(r) > 0 {
			m.setChatSearch(string(r[:len(r)-1]))
		}
	case bubbletea.KeyCtrlU:
		m.setChatSearch("")
	case bubbletea.KeySpace:
		m.setChatSearch(m.chatSearch + " ")
	case bubbletea.KeyRunes:
		m.setChatSearch(m.chatSearch + typedText(msg.Runes))
	default:
		return nil, false // arrows, Tab, ^O and friends work as usual
	}
	return nil, true
}

// sendMessageResult is delivered back to Update after a send attempt.
type sendMessageResult struct {
	err         error
	text        string
	msgID       string
	chatJID     string
	time        time.Time
	replyID     string
	replyText   string
	replySender string
}

// recipientJID builds the destination JID for the active chat.
func (m Model) recipientJID() (waTypes.JID, error) {
	return chatRecipient(m.activeChat)
}

// chatRecipient turns a chat key (phone number or full JID) into a JID.
func chatRecipient(chat string) (waTypes.JID, error) {
	if strings.Contains(chat, "@") {
		return waTypes.ParseJID(chat)
	}
	return waTypes.NewJID(chat, waTypes.DefaultUserServer), nil
}

func (m Model) sendMessageCmd() bubbletea.Cmd {
	var reply *history.Message
	if m.replyTo != nil {
		quoted := *m.replyTo
		reply = &quoted
	}
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

		out := &waE2E.Message{Conversation: &text}
		result := sendMessageResult{text: text, chatJID: m.activeChat}
		if reply != nil {
			result.replyID = reply.WaID
			result.replyText = quoteSnippet(reply.Text)
			result.replySender = m.quoteSenderKey(*reply)
			out = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        &text,
				ContextInfo: m.quoteContext(result.replyID, result.replySender, result.replyText),
			}}
		}

		resp, err := m.client.SendMessage(ctx, recipient, out)
		if err != nil {
			return sendMessageResult{err: err}
		}

		result.msgID = string(resp.ID)
		result.time = resp.Timestamp
		return result
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

// applyEdit updates an edited or deleted message in the open chat.
func (m *Model) applyEdit(chatJID, targetID, text string, deleted bool) {
	if m.step != 1 || m.activeChat != chatJID {
		return
	}
	for i := range m.chatMessages {
		msg := &m.chatMessages[i]
		if msg.WaID != targetID {
			continue
		}
		if deleted {
			msg.Text = history.DeletedText
			msg.MediaType, msg.MediaPath, msg.MediaName, msg.MediaRaw = "", "", "", nil
		} else if msg.Text != history.DeletedText {
			msg.Text = text
			msg.Edited = true
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

// isArchived reports whether a chat is archived, looking it up once and caching
// the answer. Archive events from the phone keep the cache current.
func (m *Model) isArchived(chatJID string) bool {
	if archived, ok := m.archived[chatJID]; ok {
		return archived
	}
	archived := whatsapp.IsArchived(m.client, chatJID)
	if m.archived != nil {
		m.archived[chatJID] = archived
	}
	return archived
}

// setShowArchived switches the recent chats view between active and archived
// chats.
func (m *Model) setShowArchived(show bool) {
	m.showArchived = show
	m.convSelected = 0
	m.convScroll = 0
	m.refreshConversations()
}

type archiveResult struct {
	err      error
	chatJID  string
	archived bool
	synced   bool // false when only archived on this device
}

// archiveChatCmd archives or unarchives the selected chat and syncs it to the
// phone.
func (m Model) archiveChatCmd(chat history.Conversation, archive bool) bubbletea.Cmd {
	return func() bubbletea.Msg {
		result := archiveResult{chatJID: chat.ChatJID, archived: archive}
		if m.client == nil {
			result.err = fmt.Errorf("not connected")
			return result
		}
		// Repairing out-of-sync chat settings can mean waiting for the phone.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		result.synced, result.err = whatsapp.SetArchived(ctx, m.client, chat.ChatJID, archive, chat.Timestamp)
		return result
	}
}

type archiveEventMsg whatsapp.ArchiveEvent

// waitForArchive blocks until the next archive change arrives.
func (m Model) waitForArchive() bubbletea.Cmd {
	if m.archiveEvents == nil {
		return nil
	}
	return func() bubbletea.Msg {
		event, ok := <-m.archiveEvents
		if !ok {
			return nil
		}
		return archiveEventMsg(event)
	}
}

// whatsappNumber returns the phone number for the currently selected chat.
func (m Model) whatsappNumber() (string, error) {
	var key string
	switch m.step {
	case 0:
		if m.addContactSelected() || m.selected >= len(m.filteredContacts) {
			return "", fmt.Errorf("no contact selected")
		}
		key = m.filteredContacts[m.selected].Number
	case 1:
		key = m.activeChat
	case 3:
		if len(m.chatList) == 0 || m.convSelected >= len(m.chatList) {
			return "", fmt.Errorf("no chat selected")
		}
		key = m.chatList[m.convSelected].ChatJID
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
			err := openURLCommand(ctx, target).Run()
			cancel()
			if err == nil {
				return openWhatsAppResult{target: target}
			}
			lastErr = err
		}
		return openWhatsAppResult{err: lastErr}
	}
}

// openURLCommand returns the platform's command for opening a URL with its
// default handler.
func openURLCommand(ctx context.Context, target string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", target)
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return exec.CommandContext(ctx, "xdg-open", target)
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
