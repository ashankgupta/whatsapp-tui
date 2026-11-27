package tui

import (
	"fmt"
	"strings"

	"whatsapp-tui/history"

	bubbletea "github.com/charmbracelet/bubbletea"
)

func (m Model) Init() bubbletea.Cmd {
	return bubbletea.Batch(m.waitForIncoming(), m.waitForCall(), m.waitForArchive(),
		m.waitForNames(), m.loadPeopleNamesCmd(), m.fetchGroupNamesCmd())
}

func (m Model) View() string {
	switch m.step {
	case 0:
		return m.viewContacts()
	case 1:
		return m.placeSixels(m.viewChat())
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

	// The contact dialog sits just above the footer; the chat view places it
	// in the input area itself.
	if m.form != nil && m.step != 1 {
		form := strings.Split(m.renderForm(), "\n")
		if height := m.terminalHeight - 1 - m.statusLines() - len(form); height > 0 {
			if len(lines) > height {
				lines = lines[:height]
			}
			for len(lines) < height {
				lines = append(lines, "")
			}
		}
		lines = append(lines, form...)
	}

	if height := m.terminalHeight - 1 - m.statusLines(); height > 0 {
		if len(lines) > height {
			lines = lines[:height]
		}
		for len(lines) < height {
			lines = append(lines, "")
		}
	}
	if status := m.renderStatusLine(); status != "" {
		lines = append(lines, status)
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
		m.renderFooter("↑↓  Navigate  •  Enter  Open chat  •  ^N  New contact  •  ^E  Edit  •  ^X  Delete  •  Tab  Recent  •  ^O  WhatsApp  •  q  Quit"),
		"",
		m.renderSearch(),
		"",
		m.renderSectionLabel("CONTACTS", count),
		m.renderContactList(),
	)
}

func (m Model) viewChat() string {
	hints := "Enter  Send  •  ^N  New line  •  ^A  Reply/Edit/Delete  •  ^F  Attach  •  ^T  Voice  •  ^P  Play  •  ^S  Save  •  ^R  React  •  ^E  Contact  •  Esc  Back"
	if m.savingMedia {
		hints = "↑↓  Select  •  Enter  Save to downloads/  •  Esc  Cancel"
	} else if m.reacting {
		hints = "↑↓  Select message  •  1-6  React  •  Esc  Cancel"
	} else if m.acting {
		hints = "↑↓  Select message  •  r  Reply  •  e  Edit  •  d  Delete  •  Esc  Cancel"
		if m.confirmDelete {
			hints = "y  Delete for everyone  •  any other key  Cancel"
		}
	} else if m.editing != nil {
		hints = "Enter  Save edit  •  Esc  Cancel edit"
	} else if m.replyTo != nil {
		hints = "Enter  Send reply  •  Esc  Cancel reply  •  ^A  Message actions"
	}
	if m.attaching {
		hints = "Enter  Send file  •  ^U  Clear  •  Esc  Cancel"
	}
	switch {
	case m.recorder != nil:
		hints = "Enter  Send voice message  •  Esc  Discard"
	case m.choosingAudio:
		hints = "↑↓  Select voice message or video  •  Enter  Play  •  Esc  Cancel"
	}
	input := m.renderInput()
	if m.form != nil {
		input = m.renderForm()
	}
	sections := []string{"", m.renderChatMessages(), ""}
	if banner := m.renderComposerBanner(); banner != "" && !m.selecting() {
		sections = append(sections, banner)
	} else if banner != "" {
		sections = append(sections, "")
	}
	return m.viewSections(m.renderChatHeader(), m.renderFooter(hints), append(sections, input)...)
}

func (m Model) viewChats() string {
	title, label := "Recent chats", "CHATS"
	count := fmt.Sprintf("%d", len(m.chatList))
	footer := "↑↓  Navigate  •  Enter  Open  •  s  Save contact  •  a  Archive  •  v  Archived  •  Tab  Contacts  •  ^O  WhatsApp  •  q  Quit"
	footer = "/  Search  •  " + footer
	switch {
	case m.chatSearching:
		footer = "Type to search  •  ↑↓  Navigate  •  Enter  Open  •  ^U  Clear  •  Esc  Close search"
		label, count = "RESULTS", fmt.Sprintf("%d", len(m.chatList))
	case m.chatSearch != "":
		label, count = "RESULTS", fmt.Sprintf("%d", len(m.chatList))
	case m.showArchived:
		title, label = "Archived", "ARCHIVED"
		footer = "/  Search  •  ↑↓  Navigate  •  Enter  Open  •  a  Unarchive  •  v  Back to chats  •  Esc  Back  •  q  Quit"
	case m.archivedCount > 0:
		count = fmt.Sprintf("%d  ·  %d archived", len(m.chatList), m.archivedCount)
	}

	header := m.renderBar(
		brandStyle.Render("◆ WhatsApp")+mutedStyle.Render("  /  ")+titleStyle.Render(title),
		keyStyle.Render("Tab")+mutedStyle.Render(" contacts"),
	)
	return m.viewSections(
		header,
		m.renderFooter(footer),
		"",
		m.renderChatSearch(),
		"",
		m.renderSectionLabel(label, count),
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
