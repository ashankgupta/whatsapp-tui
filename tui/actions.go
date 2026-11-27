package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"whatsapp-tui/history"

	bubbletea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// maxQuoteRunes caps how much of a quoted message is kept with a reply.
const maxQuoteRunes = 200

// editResult is delivered back to Update after an edit attempt.
type editResult struct {
	err      error
	chatJID  string
	targetID string
	text     string
}

// revokeResult is delivered back to Update after a delete-for-everyone attempt.
type revokeResult struct {
	err      error
	chatJID  string
	targetID string
}

// quoteSnippet shortens a message to what is kept as the quoted text.
func quoteSnippet(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > maxQuoteRunes {
		return string(runes[:maxQuoteRunes]) + "…"
	}
	return string(runes)
}

// quoteSenderKey is the phone number (or JID) stored as the sender of a quoted
// message: ours for our own messages, otherwise the author.
func (m Model) quoteSenderKey(target history.Message) string {
	if target.FromMe {
		return m.ownUser()
	}
	if target.SenderJID != "" {
		return target.SenderJID
	}
	return target.ChatJID
}

// quoteContext builds the reply header WhatsApp expects around a quoted message.
func (m Model) quoteContext(id, sender, text string) *waE2E.ContextInfo {
	participant := waTypes.NewJID(sender, waTypes.DefaultUserServer)
	if strings.Contains(sender, "@") {
		if jid, err := waTypes.ParseJID(sender); err == nil {
			participant = jid
		}
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(id),
		Participant:   proto.String(participant.String()),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(text)},
	}
}

// quoteLabel names the author of a quoted message for display. In a direct
// chat anyone who isn't us is the other person.
func (m Model) quoteLabel(msg history.Message) string {
	switch {
	case msg.ReplySender == "":
		return "Message"
	case msg.ReplySender == m.ownUser():
		return "You"
	case strings.HasSuffix(msg.ChatJID, "@g.us"):
		return m.chatName(msg.ReplySender)
	}
	return m.chatName(msg.ChatJID)
}

// cancelComposer drops a pending reply or edit. An edit also clears the input
// box, since it holds the old text.
func (m *Model) cancelComposer() {
	wasBottom := m.chatScroll >= m.maxChatScroll()
	if m.editing != nil {
		m.messageLines, m.currentLine = []string{""}, 0
	}
	m.replyTo, m.editing = nil, nil
	m.statusMessage = ""
	if wasBottom {
		m.chatScroll = m.maxChatScroll()
	}
}

// closeActions leaves the message picker and shows the bottom of the chat,
// where the input box is.
func (m *Model) closeActions() {
	m.acting, m.confirmDelete = false, false
	m.chatScroll = m.maxChatScroll()
}

// handleActionKey handles keys while a message is being picked to reply to,
// edit or delete.
func (m *Model) handleActionKey(msg bubbletea.KeyMsg) bubbletea.Cmd {
	if m.reactionTarget < 0 || m.reactionTarget >= len(m.chatMessages) {
		m.acting, m.confirmDelete = false, false
		return nil
	}
	target := m.chatMessages[m.reactionTarget]

	if m.confirmDelete {
		m.confirmDelete = false
		if msg.String() != "y" {
			m.statusMessage = ""
			return nil
		}
		m.chatScroll = m.reactionTarget
		m.acting = false
		m.isSending = true
		m.statusMessage = "Deleting message..."
		return m.revokeMessageCmd(target)
	}

	switch msg.String() {
	case "up", "k":
		if m.reactionTarget > 0 {
			m.reactionTarget--
		}
		m.statusMessage = ""
	case "down", "j":
		if m.reactionTarget < len(m.chatMessages)-1 {
			m.reactionTarget++
		}
		m.statusMessage = ""
	case "esc", "ctrl+a":
		m.acting = false
		m.chatScroll = m.reactionScroll
		m.statusMessage = ""

	case "r":
		if target.WaID == "" || target.Text == history.DeletedText {
			m.statusMessage = "⚠️ This message can't be replied to"
			return nil
		}
		reply := target
		m.replyTo, m.editing = &reply, nil
		m.closeActions()
		m.statusMessage = ""

	case "e":
		switch {
		case !target.FromMe:
			m.statusMessage = "⚠️ You can only edit your own messages"
		case target.WaID == "" || target.Text == history.DeletedText:
			m.statusMessage = "⚠️ This message can't be edited"
		case target.MediaType != "":
			m.statusMessage = "⚠️ Only text messages can be edited"
		case time.Since(target.Timestamp) > whatsmeow.EditWindow:
			m.statusMessage = fmt.Sprintf("⚠️ Messages can only be edited for %d minutes", int(whatsmeow.EditWindow.Minutes()))
		default:
			edit := target
			m.editing, m.replyTo = &edit, nil
			m.messageLines = strings.Split(target.Text, "\n")
			m.currentLine = len(m.messageLines) - 1
			m.closeActions()
			m.statusMessage = ""
		}

	case "d":
		switch {
		case !target.FromMe:
			m.statusMessage = "⚠️ You can only delete your own messages"
		case target.WaID == "" || target.Text == history.DeletedText:
			m.statusMessage = "⚠️ This message is already deleted"
		default:
			m.confirmDelete = true
			m.statusMessage = "⚠️ Delete this message for everyone? Press y to confirm, any other key to cancel"
		}
	}
	return nil
}

// editMessageCmd sends an edit of one of our own text messages.
func (m Model) editMessageCmd(target history.Message, text string) bubbletea.Cmd {
	return func() bubbletea.Msg {
		if m.client == nil || m.activeChat == "" {
			return editResult{err: fmt.Errorf("no chat selected")}
		}
		recipient, err := m.recipientJID()
		if err != nil {
			return editResult{err: err}
		}

		content := &waE2E.Message{Conversation: proto.String(text)}
		if target.ReplyID != "" {
			// Keep the quote on an edited reply.
			content = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(text),
				ContextInfo: m.quoteContext(target.ReplyID, target.ReplySender, target.ReplyText),
			}}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := m.client.SendMessage(ctx, recipient, m.client.BuildEdit(recipient, waTypes.MessageID(target.WaID), content)); err != nil {
			return editResult{err: err}
		}
		return editResult{chatJID: m.activeChat, targetID: target.WaID, text: text}
	}
}

// revokeMessageCmd deletes one of our own messages for everyone.
func (m Model) revokeMessageCmd(target history.Message) bubbletea.Cmd {
	return func() bubbletea.Msg {
		if m.client == nil || m.activeChat == "" {
			return revokeResult{err: fmt.Errorf("no chat selected")}
		}
		recipient, err := m.recipientJID()
		if err != nil {
			return revokeResult{err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		revoke := m.client.BuildRevoke(recipient, waTypes.EmptyJID, waTypes.MessageID(target.WaID))
		if _, err := m.client.SendMessage(ctx, recipient, revoke); err != nil {
			return revokeResult{err: err}
		}
		return revokeResult{chatJID: m.activeChat, targetID: target.WaID}
	}
}
