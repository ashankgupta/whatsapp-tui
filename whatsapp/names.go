package whatsapp

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// NameUpdate reports a change to the names WhatsApp knows for chats.
type NameUpdate struct {
	Chat          string // chat key whose name changed, with Name set
	Name          string
	ReloadPeople  bool // contact or profile names changed; reload them
	RefetchGroups bool // (re)connected; fetch group and channel names
}

// PeopleNames returns display names for people from the names WhatsApp has
// synced: the name saved in the phone's address book first, then the
// business name, then the profile name the person set themselves (shown
// with a "~" prefix like WhatsApp does). Keys match the history store:
// phone numbers for direct chats, full JIDs for hidden-user (LID) chats.
func PeopleNames(ctx context.Context, client *whatsmeow.Client) map[string]string {
	names := map[string]string{}
	if client == nil || client.Store == nil || client.Store.Contacts == nil {
		return names
	}
	all, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return names
	}

	rank := map[string]int{}
	set := func(key, name string, r int) {
		if key == "" || name == "" || r <= rank[key] {
			return
		}
		names[key], rank[key] = name, r
	}

	for jid, info := range all {
		name, r := personName(info)
		if r == 0 {
			continue
		}
		switch jid.Server {
		case types.DefaultUserServer:
			set(jid.User, name, r)
		case types.HiddenUserServer:
			set(jid.String(), name, r)
			// Group senders may be stored by their bare hidden-user ID.
			set(jid.User, name, r)
			if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
				set(pn.User, name, r)
			}
		}
	}
	return names
}

// personName picks the best name for a contact and ranks how reliable it is.
func personName(info types.ContactInfo) (string, int) {
	switch {
	case strings.TrimSpace(info.FullName) != "":
		return strings.TrimSpace(info.FullName), 4
	case strings.TrimSpace(info.FirstName) != "":
		return strings.TrimSpace(info.FirstName), 3
	case strings.TrimSpace(info.BusinessName) != "":
		return strings.TrimSpace(info.BusinessName), 2
	case strings.TrimSpace(info.PushName) != "":
		return "~ " + strings.TrimSpace(info.PushName), 1
	}
	return "", 0
}

// GroupNames fetches the names of joined groups and followed channels from
// WhatsApp. It needs a live connection.
func GroupNames(ctx context.Context, client *whatsmeow.Client) map[string]string {
	names := map[string]string{}
	if client == nil || !client.IsConnected() {
		return names
	}
	if groups, err := client.GetJoinedGroups(ctx); err == nil {
		for _, g := range groups {
			if g != nil && g.Name != "" {
				names[g.JID.String()] = g.Name
			}
		}
	}
	if channels, err := client.GetSubscribedNewsletters(ctx); err == nil {
		for _, c := range channels {
			if c != nil && c.ThreadMeta.Name.Text != "" {
				names[c.ID.String()] = c.ThreadMeta.Name.Text
			}
		}
	}
	return names
}

// ListenNames streams name changes: group renames and joins directly, and a
// reload signal when contact or profile names change.
func ListenNames(client *whatsmeow.Client) <-chan NameUpdate {
	ch := make(chan NameUpdate, 100)
	send := func(u NameUpdate) {
		select {
		case ch <- u:
		default:
		}
	}
	client.AddEventHandler(func(evt interface{}) {
		switch e := evt.(type) {
		case *events.GroupInfo:
			if e.Name != nil && e.Name.Name != "" {
				send(NameUpdate{Chat: e.JID.String(), Name: e.Name.Name})
			}
		case *events.JoinedGroup:
			if e.GroupName.Name != "" {
				send(NameUpdate{Chat: e.JID.String(), Name: e.GroupName.Name})
			}
		case *events.Contact, *events.PushName, *events.BusinessName:
			send(NameUpdate{ReloadPeople: true})
		case *events.Connected:
			send(NameUpdate{RefetchGroups: true, ReloadPeople: true})
		}
	})
	return ch
}
