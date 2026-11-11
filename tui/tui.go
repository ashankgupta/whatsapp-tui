package tui

import (
	"fmt"
	"strings"

	"whatsapp-tui/history"

	bubbletea "github.com/charmbracelet/bubbletea"
)

func (m Model) Init() bubbletea.Cmd {
	if m.incomingCalls != nil {
		return bubbletea.Batch(m.waitForIncoming(), m.waitForCall())
	}
	return m.waitForIncoming()
}

func (m Model) View() string {
	switch m.step {
	case 0:
		return m.viewContacts()
	case 1:
		return m.viewChat()
	case 3:
		return m.viewChats()
	}
	return ""
}

// viewSections stacks header, optional call banner and body, then pins the
// footer to the last terminal line.
func (m Model) viewSections(header, footer string, sections ...string) string {
	parts := []string{header}
	if m.activeCall != nil {
		parts = append(parts, m.renderCallBanner(), "")
	}
	parts = append(parts, sections...)
	lines := strings.Split(strings.Join(parts, "\n"), "\n")

	if height := m.terminalHeight - 1; height > 0 {
		if len(lines) > height {
			lines = lines[:height]
		}
		for len(lines) < height {
			lines = append(lines, "")
		}
	}
	return strings.Join(append(lines, footer), "\n")
}

func (m Model) viewContacts() string {
	header := m.renderBar(
		brandStyle.Render("◆ WhatsApp"),
		accentStyle.Render("●")+" "+secondaryStyle.Render("Online"),
	)
	count := fmt.Sprintf("%d", len(m.filteredContacts))
	if len(m.filteredContacts) != len(m.contacts) {
		count = fmt.Sprintf("%d of %d", len(m.filteredContacts), len(m.contacts))
	}

	return m.viewSections(
		header,
		m.renderFooter("↑↓  Navigate  •  Enter  Open chat  •  Tab  Recent  •  ^O  Open in WhatsApp  •  q  Quit"),
		"",
		m.renderSearch(),
		"",
		m.renderSectionLabel("CONTACTS", count),
		m.renderContactList(),
	)
}

func (m Model) viewChat() string {
	hints := "Enter  Send  •  ^N  New line  •  ^F  Attach  •  ^S  Save media  •  ^R  React  •  ^O  WhatsApp  •  Esc  Back"
	if m.savingMedia {
		hints = "↑↓  Select  •  Enter  Save to downloads/  •  Esc  Cancel"
	} else if m.reacting {
		hints = "↑↓  Select message  •  1-6  React  •  Esc  Cancel"
	}
	if m.attaching {
		hints = "Enter  Send file  •  ^U  Clear  •  Esc  Cancel"
	}
	return m.viewSections(
		m.renderChatHeader(),
		m.renderFooter(hints),
		"",
		m.renderChatMessages(),
		"",
		m.renderInput(),
	)
}

func (m Model) viewChats() string {
	header := m.renderBar(
		brandStyle.Render("◆ WhatsApp")+mutedStyle.Render("  /  ")+titleStyle.Render("Recent chats"),
		keyStyle.Render("Tab")+mutedStyle.Render(" contacts"),
	)
	return m.viewSections(
		header,
		m.renderFooter("↑↓  Navigate  •  Enter  Open  •  Tab  Contacts  •  ^O  WhatsApp  •  q  Quit"),
		"",
		m.renderSectionLabel("CHATS", fmt.Sprintf("%d", len(m.conversations))),
		m.renderChatsList(),
	)
}

// senderName resolves the display name for the sender of a message.
func (m Model) senderName(msg history.Message) string {
	if msg.FromMe {
		return "You"
	}
	if strings.Contains(msg.ChatJID, "@") && msg.SenderJID != "" {
		return m.chatName(msg.SenderJID)
	}
	return m.chatName(msg.ChatJID)
}
