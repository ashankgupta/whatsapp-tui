package tui

import (
	"context"
	"strings"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"

	"whatsapp-tui/contacts"
	"whatsapp-tui/whatsapp"
)

type (
	nameUpdateMsg   whatsapp.NameUpdate
	reloadPeopleMsg struct{}
	peopleNamesMsg  map[string]string
	groupNamesMsg   map[string]string
)

// chatName resolves a chat key to a display name. A name saved in
// contacts.json always wins, then group and channel titles and people's
// names from WhatsApp, and finally the raw number or ID.
func (m Model) chatName(key string) string {
	if key == "" {
		return "Unknown"
	}
	if c, ok := m.savedContact(key); ok {
		return c.Name
	}
	if name := m.groupNames[key]; name != "" {
		return name
	}
	if name := m.peopleNames[key]; name != "" {
		return name
	}
	id, server, hasServer := strings.Cut(key, "@")
	switch {
	case key == "status@broadcast":
		return "Status"
	case server == "g.us":
		return "Group " + id
	case server == "newsletter":
		return "Channel " + id
	case hasServer:
		return id
	}
	return key
}

// whatsappName is the name WhatsApp knows for a chat, ignoring contacts.json.
func (m Model) whatsappName(key string) string {
	if name := m.groupNames[key]; name != "" {
		return name
	}
	return m.peopleNames[key]
}

// savedContact looks up a contact from contacts.json by number.
func (m Model) savedContact(number string) (contacts.Contact, bool) {
	for _, c := range m.contacts {
		if c.Number == number {
			return c, true
		}
	}
	return contacts.Contact{}, false
}

func (m Model) waitForNames() bubbletea.Cmd {
	if m.nameEvents == nil {
		return nil
	}
	return func() bubbletea.Msg {
		update, ok := <-m.nameEvents
		if !ok {
			return nil
		}
		return nameUpdateMsg(update)
	}
}

// loadPeopleNamesCmd reads people's names from the local WhatsApp store.
func (m Model) loadPeopleNamesCmd() bubbletea.Cmd {
	if m.client == nil {
		return nil
	}
	client := m.client
	return func() bubbletea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return peopleNamesMsg(whatsapp.PeopleNames(ctx, client))
	}
}

// fetchGroupNamesCmd asks WhatsApp for group and channel names.
func (m Model) fetchGroupNamesCmd() bubbletea.Cmd {
	if m.client == nil {
		return nil
	}
	client := m.client
	return func() bubbletea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return groupNamesMsg(whatsapp.GroupNames(ctx, client))
	}
}

// handleNameMsg applies name updates. Profile name changes arrive in bursts
// (one per message), so reloads are batched into one every two seconds.
func (m *Model) handleNameMsg(msg bubbletea.Msg) bubbletea.Cmd {
	switch msg := msg.(type) {
	case nameUpdateMsg:
		cmds := []bubbletea.Cmd{m.waitForNames()}
		if msg.Chat != "" && msg.Name != "" {
			m.groupNames[msg.Chat] = msg.Name
		}
		if msg.RefetchGroups {
			cmds = append(cmds, m.fetchGroupNamesCmd())
		}
		if msg.ReloadPeople && !m.namesReloadPending {
			m.namesReloadPending = true
			cmds = append(cmds, bubbletea.Tick(2*time.Second, func(time.Time) bubbletea.Msg { return reloadPeopleMsg{} }))
		}
		return bubbletea.Batch(cmds...)
	case reloadPeopleMsg:
		m.namesReloadPending = false
		return m.loadPeopleNamesCmd()
	case peopleNamesMsg:
		if len(msg) > 0 {
			m.peopleNames = msg
		}
	case groupNamesMsg:
		for k, v := range msg {
			m.groupNames[k] = v
		}
	}
	return nil
}
