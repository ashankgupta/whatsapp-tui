package tui

import (
	"strings"

	bubbletea "github.com/charmbracelet/bubbletea"

	"whatsapp-tui/contacts"
)

type formKind int

const (
	formNewContact formKind = iota
	formEditContact
	formDeleteContact
)

type formField struct {
	label string
	value string
}

// contactForm is the small dialog used to add, rename or delete contacts, so
// nobody has to edit contacts.json by hand.
type contactForm struct {
	kind   formKind
	title  string
	number string // fixed number for edit and delete
	fields []formField
	focus  int
}

// openNewContactForm asks for a number and a name, prefilled from the
// contacts search box: a number goes into Number, anything else into Name.
func (m *Model) openNewContactForm() {
	f := &contactForm{
		kind:   formNewContact,
		title:  "New contact",
		fields: []formField{{label: "Number"}, {label: "Name"}},
	}
	if query := strings.TrimSpace(m.searchQuery); query != "" {
		if _, err := contacts.NormalizeNumber(query); err == nil {
			f.fields[0].value = query
			f.focus = 1
		} else {
			f.fields[1].value = query
		}
	}
	m.form = f
	m.statusMessage = ""
}

// openEditContactForm saves or renames the contact for a direct chat,
// prefilled with the saved name or the name WhatsApp knows.
func (m *Model) openEditContactForm(chatKey string) {
	number, err := contacts.NormalizeNumber(chatKey)
	if err != nil || strings.Contains(chatKey, "@") {
		m.statusMessage = "❌ Only chats with a phone number can be saved as contacts"
		return
	}
	title := "Save contact"
	name := strings.TrimPrefix(m.whatsappName(number), "~ ")
	if c, ok := m.savedContact(number); ok {
		title, name = "Edit contact", c.Name
	}
	m.form = &contactForm{
		kind:   formEditContact,
		title:  title + "  " + mutedStyle.Render("+"+number),
		number: number,
		fields: []formField{{label: "Name", value: name}},
	}
	m.statusMessage = ""
}

// openDeleteContactForm asks for confirmation before removing a contact.
func (m *Model) openDeleteContactForm(c contacts.Contact) {
	m.form = &contactForm{
		kind:   formDeleteContact,
		title:  "Delete " + c.Name + "  " + mutedStyle.Render("+"+c.Number) + "  from contacts?",
		number: c.Number,
	}
	m.statusMessage = ""
}

// handleFormKey handles keys while a contact dialog is open.
func (m *Model) handleFormKey(msg bubbletea.KeyMsg) bubbletea.Cmd {
	f := m.form
	switch msg.Type {
	case bubbletea.KeyEsc:
		m.closeForm()
		return nil
	case bubbletea.KeyEnter:
		if f.kind != formDeleteContact && f.focus < len(f.fields)-1 {
			f.focus++
			return nil
		}
		m.submitForm()
		return nil
	}
	if f.kind == formDeleteContact {
		if s := msg.String(); s == "y" || s == "Y" {
			m.submitForm()
		}
		return nil
	}

	field := &f.fields[f.focus]
	switch msg.Type {
	case bubbletea.KeyTab, bubbletea.KeyDown:
		f.focus = (f.focus + 1) % len(f.fields)
	case bubbletea.KeyShiftTab, bubbletea.KeyUp:
		f.focus = (f.focus + len(f.fields) - 1) % len(f.fields)
	case bubbletea.KeyBackspace:
		if r := []rune(field.value); len(r) > 0 {
			field.value = string(r[:len(r)-1])
		}
	case bubbletea.KeyCtrlU:
		field.value = ""
	case bubbletea.KeySpace:
		field.value += " "
	case bubbletea.KeyRunes:
		field.value += typedText(msg.Runes)
	}
	return nil
}

// closeForm dismisses the dialog and restores the chat scroll position.
func (m *Model) closeForm() {
	wasBottom := m.step == 1 && m.chatScroll >= m.maxChatScroll()
	m.form = nil
	m.keepChatBottom(wasBottom)
}

// submitForm validates the dialog, updates the contact list and writes
// contacts.json.
func (m *Model) submitForm() {
	f := m.form
	list := m.contacts
	var done string

	switch f.kind {
	case formNewContact, formEditContact:
		number := f.number
		nameField := f.fields[0]
		if f.kind == formNewContact {
			var err error
			number, err = contacts.NormalizeNumber(f.fields[0].value)
			if err != nil {
				f.focus = 0
				m.statusMessage = "❌ " + err.Error()
				return
			}
			nameField = f.fields[1]
		}
		name := singleLine(nameField.value)
		if name == "" {
			name = strings.TrimPrefix(m.whatsappName(number), "~ ")
		}
		if name == "" {
			f.focus = len(f.fields) - 1
			m.statusMessage = "❌ Enter a name"
			return
		}
		list = contacts.Upsert(list, contacts.Contact{Name: name, Number: number})
		done = "✅ Saved " + name + " to contacts"
	case formDeleteContact:
		c, _ := m.savedContact(f.number)
		list = contacts.Remove(list, f.number)
		done = "✅ Deleted " + c.Name + " from contacts"
	}

	if err := contacts.SaveContacts(m.contactsPath, list); err != nil {
		m.statusMessage = "❌ Could not save contacts: " + err.Error()
		return
	}
	m.contacts = list
	m.filterContacts()
	m.adjustSelection()
	m.closeForm()
	m.statusMessage = done
}

// formLines is the dialog's inner height, used to size the other sections.
func (m Model) formLines() int {
	if m.form == nil {
		return 0
	}
	return 1 + len(m.form.fields)
}

// renderForm draws the contact dialog as a bordered box.
func (m Model) renderForm() string {
	f := m.form
	innerWidth := m.terminalWidth - 4
	if innerWidth < 20 {
		innerWidth = 20
	}
	lines := []string{titleStyle.Render(f.title)}
	labelWidth := 0
	for _, field := range f.fields {
		labelWidth = max(labelWidth, len(field.label))
	}
	for i, field := range f.fields {
		label := field.label + strings.Repeat(" ", labelWidth-len(field.label)) + "  "
		value := truncate(field.value, max(4, innerWidth-labelWidth-6))
		if i == f.focus {
			line := keyStyle.Render(label) + normalStyle.Render(value) + accentStyle.Render("▏")
			if field.value == "" && field.label == "Number" {
				line += mutedStyle.Render("with country code, e.g. +91 98765 43210")
			}
			lines = append(lines, line)
		} else {
			lines = append(lines, mutedStyle.Render(label)+secondaryStyle.Render(value))
		}
	}
	return inputBarStyle.Width(innerWidth).Render(strings.Join(lines, "\n"))
}

// formHints are the footer hints shown while the dialog is open.
func (m Model) formHints() string {
	switch {
	case m.form.kind == formDeleteContact:
		return "Enter  Delete  •  Esc  Cancel"
	case len(m.form.fields) > 1:
		return "Enter  Next / Save  •  Tab  Switch field  •  ^U  Clear  •  Esc  Cancel"
	default:
		return "Enter  Save  •  ^U  Clear  •  Esc  Cancel"
	}
}
