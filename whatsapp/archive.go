package whatsapp

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// ArchiveEvent reports that a chat was archived or unarchived, usually from
// the phone or another linked device.
type ArchiveEvent struct {
	Chat     string
	Archived bool
}

// chatJID converts a chat key (phone number or full group JID) to a JID.
func chatJID(chat string) (types.JID, error) {
	if strings.Contains(chat, "@") {
		return types.ParseJID(chat)
	}
	return types.NewJID(chat, types.DefaultUserServer), nil
}

// keyForJID converts a JID from an app state event back to the chat key used
// by the history store, resolving hidden-user (LID) JIDs to phone numbers.
func keyForJID(ctx context.Context, client *whatsmeow.Client, jid types.JID) string {
	switch jid.Server {
	case types.DefaultUserServer:
		return jid.User
	case types.HiddenUserServer:
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
			return pn.User
		}
	}
	return jid.String()
}

// lidFor returns the hidden-user JID for a phone number JID, if known.
func lidFor(ctx context.Context, client *whatsmeow.Client, jid types.JID) (types.JID, bool) {
	if jid.Server != types.DefaultUserServer {
		return types.JID{}, false
	}
	lid, err := client.Store.LIDs.GetLIDForPN(ctx, jid)
	if err != nil || lid.IsEmpty() {
		return types.JID{}, false
	}
	return lid, true
}

// ListenArchive registers an event handler for archive changes and streams
// them on the returned channel.
func ListenArchive(client *whatsmeow.Client) <-chan ArchiveEvent {
	ch := make(chan ArchiveEvent, 100)
	client.AddEventHandler(func(evt interface{}) {
		archive, ok := evt.(*events.Archive)
		if !ok || archive.Action == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		select {
		case ch <- ArchiveEvent{
			Chat:     keyForJID(ctx, client, archive.JID),
			Archived: archive.Action.GetArchived(),
		}:
		default:
		}
	})
	return ch
}

// IsArchived reports whether a chat is archived, using the chat settings that
// whatsmeow keeps in sync with the phone. Direct chats may be stored under
// either the phone number or the hidden-user JID; the latter wins when both
// exist because newer WhatsApp versions use it.
func IsArchived(client *whatsmeow.Client, chat string) bool {
	if client == nil || client.Store == nil || client.Store.ChatSettings == nil {
		return false
	}
	jid, err := chatJID(chat)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if lid, ok := lidFor(ctx, client, jid); ok {
		if settings, err := client.Store.ChatSettings.GetChatSettings(ctx, lid); err == nil && settings.Found {
			return settings.Archived
		}
	}
	settings, err := client.Store.ChatSettings.GetChatSettings(ctx, jid)
	return err == nil && settings.Found && settings.Archived
}

// SetArchived archives or unarchives a chat for this account, syncing the
// change to the phone and other linked devices.
func SetArchived(ctx context.Context, client *whatsmeow.Client, chat string, archived bool, lastMessage time.Time) error {
	jid, err := chatJID(chat)
	if err != nil {
		return err
	}
	if lid, ok := lidFor(ctx, client, jid); ok {
		jid = lid
	}
	return client.SendAppState(ctx, appstate.BuildArchive(jid, archived, lastMessage, nil))
}
