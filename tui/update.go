package tui

import (
	"path/filepath"
	"strings"
	"time"

	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"

	bubbletea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.WindowSizeMsg:
		m.terminalWidth = msg.Width
		m.terminalHeight = msg.Height
		m.windowHeight = msg.Height - 10
		if m.windowHeight < 3 {
			m.windowHeight = 3
		}
		if m.step == 0 {
			m.adjustScroll()
		} else if m.step == 1 {
			m.chatScroll = m.maxChatScroll()
		} else if m.step == 3 {
			m.adjustConvScroll()
		}
		return m, nil

	case bubbletea.KeyMsg:
		if msg.Type == bubbletea.KeyCtrlC {
			return m, bubbletea.Quit
		}

		if m.activeCall != nil {
			switch msg.String() {
			case "ctrl+d":
				wasBottom := m.step == 1 && !m.reacting && m.chatScroll >= m.maxChatScroll()
				call := *m.activeCall
				m.activeCall = nil
				m.statusMessage = "Rejecting call..."
				m.keepChatBottom(wasBottom)
				return m, m.rejectCallCmd(call)
			case "esc":
				wasBottom := m.step == 1 && !m.reacting && m.chatScroll >= m.maxChatScroll()
				m.activeCall = nil
				m.statusMessage = "Call alert hidden"
				m.keepChatBottom(wasBottom)
				return m, nil
			}
		}

		if m.step == 1 && !m.attaching && m.savingMedia {
			switch msg.String() {
			case "enter":
				if cmd := m.saveSelectedMedia(); cmd != nil {
					return m, cmd
				}
				return m, nil
			case "up", "k":
				if m.saveTarget > 0 {
					m.saveTarget--
				}
			case "down", "j":
				if m.saveTarget < len(m.chatMessages)-1 {
					m.saveTarget++
				}
			case "esc", "ctrl+s":
				m.savingMedia = false
				m.chatScroll = m.saveScroll
				m.statusMessage = ""
			}
			return m, nil
		}

		if m.step == 1 && !m.attaching && msg.String() == "ctrl+s" {
			if cmd := m.saveSelectedMedia(); cmd != nil {
				return m, cmd
			}
			return m, nil
		}

		if m.step == 1 && m.reacting {
			switch msg.String() {
			case "1", "2", "3", "4", "5", "6":
				if m.reactionTarget < 0 || m.reactionTarget >= len(m.chatMessages) {
					m.reacting = false
					m.statusMessage = "No message to react to"
					return m, nil
				}
				emoji := reactionEmojis[int(msg.Runes[0]-'1')]
				target := m.chatMessages[m.reactionTarget]
				send := emoji
				if target.Reactions[m.ownUser()] == emoji {
					send = ""
				}
				m.chatScroll = m.reactionTarget
				m.reacting = false
				m.statusMessage = "Reacting..."
				return m, m.sendReactionCmd(target, send)
			case "up", "k":
				if m.reactionTarget > 0 {
					m.reactionTarget--
				}
			case "down", "j":
				if m.reactionTarget < len(m.chatMessages)-1 {
					m.reactionTarget++
				}
			case "esc", "ctrl+r":
				m.reacting = false
				m.chatScroll = m.reactionScroll
				m.statusMessage = ""
			}
			return m, nil
		}

		if m.step == 1 && m.attaching {
			switch msg.Type {
			case bubbletea.KeyEsc:
				wasBottom := m.chatScroll >= m.maxChatScroll()
				m.attaching = false
				m.attachInput = ""
				m.statusMessage = "File attach cancelled"
				if wasBottom {
					m.chatScroll = m.maxChatScroll()
				}
			case bubbletea.KeyEnter:
				path, err := resolveLocalPath(m.attachInput)
				if err != nil {
					m.statusMessage = "❌ " + err.Error()
					return m, nil
				}
				wasBottom := m.chatScroll >= m.maxChatScroll()
				m.attaching = false
				m.attachInput = ""
				m.isUploading = true
				m.statusMessage = "Uploading " + filepath.Base(path) + "..."
				if wasBottom {
					m.chatScroll = m.maxChatScroll()
				}
				return m, m.sendFileCmd(path)
			case bubbletea.KeyBackspace:
				runes := []rune(m.attachInput)
				if len(runes) > 0 {
					m.attachInput = string(runes[:len(runes)-1])
				}
			case bubbletea.KeySpace:
				m.attachInput += " "
			case bubbletea.KeyRunes:
				m.attachInput += string(msg.Runes)
			case bubbletea.KeyCtrlU:
				m.attachInput = ""
			}
			return m, nil
		}

		if m.step == 1 && msg.String() == "ctrl+f" {
			if m.isSending || m.isUploading {
				m.statusMessage = "Please wait for the current send to finish"
				return m, nil
			}
			wasBottom := m.chatScroll >= m.maxChatScroll()
			m.attaching = true
			m.attachInput = ""
			m.statusMessage = "Type a file path and press Enter"
			if wasBottom {
				m.chatScroll = m.maxChatScroll()
			}
			return m, nil
		}

		if m.step == 1 && msg.String() == "ctrl+r" {
			if len(m.chatMessages) == 0 {
				m.statusMessage = "No message to react to"
				return m, nil
			}
			m.reacting = true
			m.reactionTarget = len(m.chatMessages) - 1
			m.reactionScroll = m.chatScroll
			m.statusMessage = ""
			return m, nil
		}

		if msg.String() == "ctrl+o" {
			number, err := m.whatsappNumber()
			if err != nil {
				m.statusMessage = "❌ " + err.Error()
				return m, nil
			}
			m.statusMessage = "Opening WhatsApp chat..."
			return m, openWhatsAppCmd(number)
		}

		if msg.String() == "q" && m.step != 1 && !(m.step == 0 && m.searchQuery != "") {
			return m, bubbletea.Quit
		}

		switch msg.Type {
		case bubbletea.KeyCtrlN:
			if m.step == 1 {
				wasBottom := m.chatScroll >= m.maxChatScroll()
				m.messageLines = append(m.messageLines, "")
				m.currentLine = len(m.messageLines) - 1
				if wasBottom {
					m.chatScroll = m.maxChatScroll()
				}
				return m, nil
			}

		case bubbletea.KeyEnter:
			switch m.step {
			case 0:
				if len(m.filteredContacts) == 0 {
					m.statusMessage = "No contacts to select"
					return m, nil
				}
				m.openChat(m.filteredContacts[m.selected].Number)
			case 1:
				if m.isSending || m.isUploading {
					m.statusMessage = "Please wait for the current send to finish"
					return m, nil
				}
				text := strings.TrimSpace(strings.Join(m.messageLines, "\n"))
				if text == "" {
					m.statusMessage = "Message cannot be empty"
					return m, nil
				}
				m.isSending = true
				m.statusMessage = "Sending message..."
				return m, m.sendMessageCmd()
			case 3:
				if len(m.conversations) > 0 && m.convSelected < len(m.conversations) {
					m.openChat(m.conversations[m.convSelected].ChatJID)
				}
			}

		case bubbletea.KeyTab:
			if m.step == 0 {
				m.refreshConversations()
				m.step = 3
				m.convSelected = 0
				m.convScroll = 0
				m.statusMessage = ""
			} else if m.step == 3 {
				m.step = 0
			}

		case bubbletea.KeyUp:
			switch m.step {
			case 0:
				if m.selected > 0 {
					m.selected--
					m.adjustScroll()
				}
			case 1:
				if m.chatScroll > 0 {
					m.chatScroll--
				}
			case 3:
				if m.convSelected > 0 {
					m.convSelected--
					m.adjustConvScroll()
				}
			}

		case bubbletea.KeyDown:
			switch m.step {
			case 0:
				if m.selected < len(m.filteredContacts)-1 {
					m.selected++
					m.adjustScroll()
				}
			case 1:
				if m.chatScroll < m.maxChatScroll() {
					m.chatScroll++
				}
			case 3:
				if m.convSelected < len(m.conversations)-1 {
					m.convSelected++
					m.adjustConvScroll()
				}
			}

		case bubbletea.KeyEsc:
			switch m.step {
			case 0:
				m.searchQuery = ""
				m.filteredContacts = m.contacts
				m.selected = 0
				m.scrollOffset = 0
			case 1:
				m.step = m.returnStep
				m.statusMessage = ""
				if m.step == 3 {
					m.refreshConversations()
				}
			case 3:
				m.step = 0
			}

		case bubbletea.KeyBackspace:
			if m.step == 0 && len(m.searchQuery) > 0 {
				m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				m.filterContacts()
				m.adjustSelection()
			} else if m.step == 1 {
				wasBottom := m.chatScroll >= m.maxChatScroll()
				before := len(m.messageLines)
				m.deleteInputRune()
				if wasBottom && len(m.messageLines) < before {
					m.chatScroll = m.maxChatScroll()
				}
			}

		case bubbletea.KeySpace:
			if m.step == 0 {
				m.searchQuery += " "
				m.filterContacts()
				m.adjustSelection()
			} else if m.step == 1 {
				m.appendInput(" ")
			}

		case bubbletea.KeyRunes:
			if m.step == 0 {
				m.searchQuery += string(msg.Runes)
				m.filterContacts()
				m.adjustSelection()
			} else if m.step == 1 {
				m.appendInput(string(msg.Runes))
			}
		}

	case incomingMessageMsg:
		chatJID := msg.Chat
		if msg.IsReaction {
			if m.store != nil {
				if err := m.store.SaveReaction(chatJID, msg.TargetID, msg.Sender, msg.Text, msg.Time); err != nil {
					m.statusMessage = "Could not save reaction: " + err.Error()
				}
			}
			wasBottom := m.step == 1 && !m.reacting && chatJID == m.activeChat && m.chatScroll >= m.maxChatScroll()
			m.applyReaction(chatJID, msg.TargetID, msg.Sender, msg.Text)
			m.keepChatBottom(wasBottom)
			if m.step == 1 && chatJID == m.activeChat && !msg.FromMe {
				m.statusMessage = "Reaction from " + m.chatName(chatJID)
			}
			return m, m.waitForIncoming()
		}

		wasBottom := m.step == 1 && !m.reacting && chatJID == m.activeChat && m.chatScroll >= m.maxChatScroll()

		record := history.Message{
			ChatJID:   chatJID,
			SenderJID: msg.Sender,
			FromMe:    msg.FromMe,
			Text:      msg.Text,
			Timestamp: msg.Time,
			WaID:      msg.ID,
			MediaType: msg.MediaType,
			MediaName: msg.MediaName,
			MediaRaw:  encodeMediaRaw(msg.Raw),
		}
		if record.Timestamp.IsZero() {
			record.Timestamp = time.Now()
		}

		inserted := true
		if m.store != nil {
			var err error
			inserted, err = m.store.Save(record)
			if err != nil {
				m.statusMessage = "Could not save message: " + err.Error()
			}
		}

		willDownload := inserted && msg.MediaType == "image" && msg.Raw != nil && record.WaID != "" && m.client != nil
		if willDownload {
			m.mediaPending[record.WaID] = true
			delete(m.mediaErrors, record.WaID)
		}

		if inserted {
			if m.step == 1 && chatJID == m.activeChat {
				m.chatMessages = append(m.chatMessages, record)
				m.keepChatBottom(wasBottom)
			} else if !msg.FromMe {
				m.unread[chatJID]++
				m.statusMessage = "New message from " + m.chatName(chatJID)
			}
			m.refreshConversations()
		}

		cmd := m.waitForIncoming()
		if willDownload {
			cmd = bubbletea.Batch(cmd, m.downloadImageCmd(record, msg.Raw))
		}
		return m, cmd

	case incomingCallMsg:
		wasBottom := m.step == 1 && !m.reacting && m.chatScroll >= m.maxChatScroll()
		event := whatsapp.CallEvent(msg)
		switch event.Type {
		case "offer":
			m.activeCall = &event
			m.statusMessage = "Incoming WhatsApp call"
		case "end":
			if m.activeCall != nil && (event.CallID == "" || event.CallID == m.activeCall.CallID) {
				m.activeCall = nil
				m.statusMessage = "Call ended"
			}
		case "answered":
			if m.activeCall != nil && (event.CallID == "" || event.CallID == m.activeCall.CallID) {
				m.activeCall = nil
				m.statusMessage = "Call answered on another device"
			}
		}
		m.keepChatBottom(wasBottom)
		if m.step == 0 {
			m.adjustScroll()
		} else if m.step == 3 {
			m.adjustConvScroll()
		}
		return m, m.waitForCall()

	case sendMessageResult:
		m.isSending = false

		if msg.err != nil {
			m.statusMessage = "❌ Failed to send: " + msg.err.Error()
			return m, nil
		}

		timestamp := msg.time
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		record := history.Message{
			ChatJID:   msg.chatJID,
			FromMe:    true,
			Text:      msg.text,
			Timestamp: timestamp,
			WaID:      msg.msgID,
		}

		inserted := true
		if m.store != nil {
			var err error
			inserted, err = m.store.Save(record)
			if err != nil {
				m.statusMessage = "Sent, but could not save history: " + err.Error()
			}
		}

		if inserted && msg.chatJID == m.activeChat {
			m.chatMessages = append(m.chatMessages, record)
		}

		if msg.chatJID == m.activeChat {
			m.messageLines = []string{""}
			m.currentLine = 0
			if !m.reacting {
				m.chatScroll = m.maxChatScroll()
			}
		}
		m.refreshConversations()

		if m.statusMessage == "" || !strings.HasPrefix(m.statusMessage, "Sent, but") {
			m.statusMessage = "✅ Message sent"
		}

	case reactionResult:
		if msg.err != nil {
			m.statusMessage = "❌ Reaction failed: " + msg.err.Error()
			return m, nil
		}
		if m.store != nil {
			if err := m.store.SaveReaction(msg.chatJID, msg.targetID, msg.sender, msg.emoji, time.Now()); err != nil {
				m.statusMessage = "Sent, but could not save reaction: " + err.Error()
				return m, nil
			}
		}
		wasBottom := m.step == 1 && !m.reacting && msg.chatJID == m.activeChat && m.chatScroll >= m.maxChatScroll()
		m.applyReaction(msg.chatJID, msg.targetID, msg.sender, msg.emoji)
		m.keepChatBottom(wasBottom)
		if msg.emoji == "" {
			m.statusMessage = "Reaction removed"
		} else {
			m.statusMessage = "✅ Reaction sent " + msg.emoji
		}

	case mediaResult:
		wasBottom := m.step == 1 && msg.chatJID == m.activeChat && m.chatScroll >= m.maxChatScroll()
		delete(m.mediaPending, msg.waID)
		if msg.err != nil {
			hasMedia := m.hasMediaPath(msg.waID)
			if !hasMedia && m.store != nil {
				if path, err := m.store.MediaPath(msg.waID); err == nil && path != "" {
					hasMedia = true
				}
			}
			if !hasMedia {
				m.mediaErrors[msg.waID] = "download failed"
				if m.step == 1 && msg.chatJID == m.activeChat {
					m.statusMessage = "❌ Could not download image: " + msg.err.Error()
				}
			}
			m.keepChatBottom(wasBottom)
			return m, nil
		}

		if m.store != nil {
			if err := m.store.SetMediaPath(msg.waID, msg.mediaType, msg.path, msg.mediaName); err != nil {
				m.statusMessage = "Image saved, but could not update history: " + err.Error()
			}
		}
		delete(m.mediaErrors, msg.waID)
		if msg.note != "" && m.step == 1 && msg.chatJID == m.activeChat {
			m.statusMessage = "⚠️ " + msg.note
		}
		if m.step == 1 && msg.chatJID == m.activeChat {
			for i := range m.chatMessages {
				if m.chatMessages[i].WaID != msg.waID {
					continue
				}
				m.chatMessages[i].MediaType = msg.mediaType
				m.chatMessages[i].MediaPath = msg.path
				m.chatMessages[i].MediaName = msg.mediaName
				break
			}
			if m.statusMessage == "" {
				m.statusMessage = "✅ Image loaded"
			}
			if maxScroll := m.maxChatScroll(); m.chatScroll > maxScroll {
				m.chatScroll = maxScroll
			}
			m.keepChatBottom(wasBottom)
		}

	case mediaSaveResult:
		m.isSavingMedia = false
		if msg.err != nil {
			m.statusMessage = "❌ Could not save media: " + msg.err.Error()
			return m, nil
		}

		wasBottom := m.step == 1 && msg.chatJID == m.activeChat && m.chatScroll >= m.maxChatScroll()
		if msg.cachedPath != "" && msg.waID != "" {
			if m.store != nil {
				if err := m.store.SetMediaPath(msg.waID, msg.mediaType, msg.cachedPath, msg.mediaName); err != nil {
					m.statusMessage = "Saved to " + msg.path + " (history update failed)"
					m.keepChatBottom(wasBottom)
					return m, nil
				}
			}
			if m.step == 1 && msg.chatJID == m.activeChat {
				for i := range m.chatMessages {
					if m.chatMessages[i].WaID != msg.waID {
						continue
					}
					m.chatMessages[i].MediaPath = msg.cachedPath
					m.chatMessages[i].MediaName = msg.mediaName
					break
				}
			}
			delete(m.mediaPending, msg.waID)
			delete(m.mediaErrors, msg.waID)
		}
		m.statusMessage = "✅ Saved to " + msg.path
		m.keepChatBottom(wasBottom)

	case fileSendResult:
		m.isUploading = false
		if msg.err != nil {
			m.statusMessage = "❌ File send failed: " + msg.err.Error()
			return m, nil
		}

		timestamp := msg.time
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		record := history.Message{
			ChatJID:   msg.chatJID,
			FromMe:    true,
			Text:      msg.text,
			Timestamp: timestamp,
			WaID:      msg.msgID,
			MediaType: msg.mediaType,
			MediaPath: msg.mediaPath,
			MediaName: msg.mediaName,
		}

		inserted := true
		if m.store != nil {
			var err error
			inserted, err = m.store.Save(record)
			if err != nil {
				m.statusMessage = "Sent, but could not save history: " + err.Error()
			} else if !inserted && msg.msgID != "" {
				if err := m.store.SetMediaPath(msg.msgID, msg.mediaType, msg.mediaPath, msg.mediaName); err != nil {
					m.statusMessage = "Sent, but could not update history: " + err.Error()
				}
			}
		}

		if msg.msgID != "" && msg.mediaPath != "" {
			delete(m.mediaPending, msg.msgID)
			delete(m.mediaErrors, msg.msgID)
		}

		if msg.chatJID == m.activeChat {
			if inserted {
				m.chatMessages = append(m.chatMessages, record)
			} else if msg.msgID != "" {
				for i := range m.chatMessages {
					if m.chatMessages[i].WaID != msg.msgID {
						continue
					}
					m.chatMessages[i].MediaType = msg.mediaType
					m.chatMessages[i].MediaPath = msg.mediaPath
					m.chatMessages[i].MediaName = msg.mediaName
					break
				}
			}
			if !m.reacting {
				m.chatScroll = m.maxChatScroll()
			}
		}
		m.refreshConversations()

		if msg.warn != "" {
			m.statusMessage = "⚠️ " + msg.warn
		} else if m.statusMessage == "" || !strings.HasPrefix(m.statusMessage, "Sent, but") {
			m.statusMessage = "✅ File sent"
		}

	case rejectCallResult:
		if msg.err != nil {
			m.statusMessage = "❌ Could not reject call: " + msg.err.Error()
		} else {
			m.statusMessage = "✅ Call rejected"
		}

	case openWhatsAppResult:
		if msg.err != nil {
			m.statusMessage = "❌ Could not open WhatsApp: " + msg.err.Error()
		} else {
			m.statusMessage = "✅ Opened WhatsApp chat"
		}
	}

	return m, nil
}

// appendInput adds text to the current input line.
func (m *Model) appendInput(text string) {
	if len(m.messageLines) == 0 {
		m.messageLines = []string{""}
		m.currentLine = 0
	}
	if m.currentLine >= len(m.messageLines) {
		m.currentLine = len(m.messageLines) - 1
	}
	m.messageLines[m.currentLine] += text
}

// deleteInputRune removes the last rune of the current input line, or merges
// the line into the previous one when it is empty.
func (m *Model) deleteInputRune() {
	if len(m.messageLines) == 0 {
		m.messageLines = []string{""}
		m.currentLine = 0
		return
	}
	if m.currentLine >= len(m.messageLines) {
		m.currentLine = len(m.messageLines) - 1
	}

	line := []rune(m.messageLines[m.currentLine])
	if len(line) > 0 {
		m.messageLines[m.currentLine] = string(line[:len(line)-1])
		return
	}

	if m.currentLine > 0 {
		m.messageLines = m.messageLines[:m.currentLine]
		m.currentLine--
	}
}
