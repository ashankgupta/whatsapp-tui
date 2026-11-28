package tui

// Sending and receiving outside the chat screen, for `whatsapp-tui send`.
// The command line uses these directly when the app isn't open, and the open
// app runs them for it when it is (see main.go).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	bubbletea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"

	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/whatsapp"
)

// ErrNotSaved means a message was sent but couldn't be saved to history.
var ErrNotSaved = errors.New("sent, but could not save it to history")

// SendText sends a text message to a chat and saves it to history.
func SendText(ctx context.Context, client *whatsmeow.Client, store *history.Store, chat, text string) (history.Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return history.Message{}, fmt.Errorf("message is empty")
	}
	if client == nil {
		return history.Message{}, fmt.Errorf("not connected")
	}
	recipient, err := chatRecipient(chat)
	if err != nil {
		return history.Message{}, err
	}
	resp, err := client.SendMessage(ctx, recipient, &waE2E.Message{Conversation: &text})
	if err != nil {
		return history.Message{}, err
	}
	record := history.Message{
		ChatJID:   chat,
		FromMe:    true,
		Text:      text,
		Timestamp: resp.Timestamp,
		WaID:      string(resp.ID),
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	if store != nil {
		if _, err := store.Save(record); err != nil {
			return record, fmt.Errorf("%w: %v", ErrNotSaved, err)
		}
	}
	return record, nil
}

// SendFile sends a local file (photos as images, anything else as a
// document) with an optional caption, and saves it to history.
func SendFile(ctx context.Context, client *whatsmeow.Client, store *history.Store, chat, path, caption string) (history.Message, error) {
	result := sendFile(ctx, client, chat, path, strings.TrimSpace(caption))
	if result.err != nil {
		return history.Message{}, result.err
	}
	record := history.Message{
		ChatJID:   chat,
		FromMe:    true,
		Text:      result.text,
		Timestamp: result.time,
		WaID:      result.msgID,
		MediaType: result.mediaType,
		MediaPath: result.mediaPath,
		MediaName: result.mediaName,
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	if store != nil {
		if _, err := store.Save(record); err != nil {
			return record, fmt.Errorf("%w: %v", ErrNotSaved, err)
		}
	}
	return record, nil
}

// SaveIncoming stores a received message, reaction, edit or delete in
// history. The command line uses it so messages that arrive while it is
// connected aren't lost: WhatsApp delivers each message to a device once.
func SaveIncoming(store *history.Store, msg whatsapp.IncomingMessage) error {
	if store == nil {
		return nil
	}
	switch {
	case msg.IsReaction:
		return store.SaveReaction(msg.Chat, msg.TargetID, msg.Sender, msg.Text, msg.Time)
	case msg.IsRevoke:
		_, err := store.MarkDeleted(msg.TargetID)
		return err
	case msg.IsEdit:
		_, err := store.EditText(msg.TargetID, msg.Text)
		return err
	}
	_, err := store.Save(incomingRecord(msg))
	return err
}

// incomingRecord converts a received message to a history row.
func incomingRecord(msg whatsapp.IncomingMessage) history.Message {
	record := history.Message{
		ChatJID:   msg.Chat,
		SenderJID: msg.Sender,
		FromMe:    msg.FromMe,
		Text:      msg.Text,
		Timestamp: msg.Time,
		WaID:      msg.ID,
		MediaType: msg.MediaType,
		MediaName: msg.MediaName,
		MediaRaw:  encodeMediaRaw(msg.Raw),

		ReplyID:     msg.ReplyID,
		ReplyText:   msg.ReplyText,
		ReplySender: msg.ReplySender,
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now()
	}
	return record
}

// externalSentMsg tells the open app about a message sent from the command
// line, so it shows up in the chat right away.
type externalSentMsg struct {
	record history.Message
	to     string
}

// ExternalSent wraps a message sent from the command line for Program.Send.
func ExternalSent(record history.Message, to string) bubbletea.Msg {
	return externalSentMsg{record: record, to: to}
}

// handleExternalSent shows a message the command line sent through the app.
func (m *Model) handleExternalSent(msg externalSentMsg) {
	rec := msg.record
	if m.step == 1 && rec.ChatJID == m.activeChat {
		found := false
		for _, existing := range m.chatMessages {
			if rec.WaID != "" && existing.WaID == rec.WaID {
				found = true
				break
			}
		}
		if !found {
			wasBottom := !m.selecting() && m.chatScroll >= m.maxChatScroll()
			m.chatMessages = append(m.chatMessages, rec)
			m.keepChatBottom(wasBottom)
		}
	}
	m.refreshConversations()
	m.statusMessage = "✅ Sent to " + msg.to + " from the command line"
}

// ResolveChat finds the chat a user means: "me", a phone number, a JID, or a
// name from contacts.json, the phone's address book, or a group or channel
// title. Names match case-insensitively, exactly first and then by part of
// the name. It returns the chat key and a display name. Group titles come
// from the store's cache when possible; fetched ones are cached there.
func ResolveChat(ctx context.Context, client *whatsmeow.Client, store *history.Store, saved []contacts.Contact, query string) (string, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", "", fmt.Errorf("no recipient given")
	}

	switch strings.ToLower(query) {
	case "me", "self", "myself":
		if client == nil || client.Store == nil || client.Store.ID == nil {
			return "", "", fmt.Errorf("not logged in")
		}
		return client.Store.ID.User, "yourself", nil
	}

	// "Name (+number)", as shown in errors and completion: use the number.
	if open := strings.LastIndex(query, "(+"); open > 0 && strings.HasSuffix(query, ")") {
		if number, err := contacts.NormalizeNumber(query[open+1 : len(query)-1]); err == nil {
			return number, strings.TrimSpace(query[:open]) + " (+" + number + ")", nil
		}
	}

	if strings.Contains(query, "@") {
		jid, err := waTypes.ParseJID(query)
		if err != nil {
			return "", "", fmt.Errorf("invalid WhatsApp ID %q: %w", query, err)
		}
		if jid.Server == waTypes.DefaultUserServer {
			return jid.User, "+" + jid.User, nil
		}
		return jid.String(), jid.String(), nil
	}

	if !strings.ContainsFunc(query, unicode.IsLetter) {
		number, err := contacts.NormalizeNumber(query)
		if err != nil {
			return "", "", err
		}
		return number, "+" + number, nil
	}

	// Saved contacts first, so they win over WhatsApp names.
	var all []nameCandidate
	seen := map[string]bool{}
	add := func(key, name string) {
		name = strings.TrimSpace(strings.TrimPrefix(name, "~ "))
		id := key + "\x00" + strings.ToLower(name)
		if key == "" || name == "" || seen[id] {
			return
		}
		seen[id] = true
		all = append(all, nameCandidate{key, name})
	}
	for _, c := range saved {
		add(c.Number, c.Name)
	}
	if client != nil {
		addSorted(phoneKeys(whatsapp.PeopleNames(ctx, client)), add)
	}
	if store != nil {
		if cached, err := store.ChatNames(); err == nil {
			addSorted(cached, add)
		}
	}

	lower := strings.ToLower(query)
	exact := func(name string) bool { return name == lower }
	partial := func(name string) bool { return strings.Contains(name, lower) }

	// An exact person name needs no network. Otherwise fetch group and
	// channel titles (a round trip) and look again, exact before partial.
	matches := matchNames(all, exact)
	if len(matches) == 0 && client != nil {
		groups := whatsapp.GroupNames(ctx, client)
		if store != nil {
			_ = store.SaveChatNames(groups)
		}
		addSorted(groups, add)
		matches = matchNames(all, exact)
	}
	if len(matches) == 0 {
		matches = matchNames(all, partial)
	}

	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("no contact, chat or group called %q; use a phone number with country code instead", query)
	case 1:
		return matches[0].key, describeChat(matches[0].name, matches[0].key), nil
	}
	var options []string
	for i, c := range matches {
		if i == 6 {
			options = append(options, fmt.Sprintf("and %d more", len(matches)-i))
			break
		}
		options = append(options, describeChat(c.name, c.key))
	}
	return "", "", fmt.Errorf("%q matches more than one chat: %s. Use the full name or the number", query, strings.Join(options, ", "))
}

type nameCandidate struct{ key, name string }

// addSorted adds names in a stable order.
func addSorted(names map[string]string, add func(key, name string)) {
	keys := make([]string, 0, len(names))
	for key := range names {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(key, names[key])
	}
}

// Suggestion is one shell-completion choice for the send command.
type Suggestion struct {
	Word        string // what gets inserted
	Description string // shown next to it
}

// Suggestions lists who `whatsapp-tui send` can send to, for shell
// completion. A name matches when it, or any word in it, starts with prefix
// (ignoring case); numbers match by their digits. Saved contacts come first.
// A name shared by several people is offered as "Name (+number)", which
// ResolveChat understands, so the completed word always picks one chat.
func Suggestions(saved []contacts.Contact, people, groups map[string]string, prefix string, limit int) []Suggestion {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	digits := strings.TrimLeft(strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, prefix), "0")
	numeric := prefix != "" && !strings.ContainsFunc(prefix, unicode.IsLetter)

	var all []nameCandidate
	seen := map[string]bool{}
	add := func(key, name string) {
		name = strings.TrimSpace(strings.TrimPrefix(name, "~ "))
		if key == "" || name == "" || seen[key] {
			return
		}
		seen[key] = true
		all = append(all, nameCandidate{key, name})
	}
	for _, c := range saved {
		add(c.Number, c.Name)
	}
	people = phoneKeys(people)
	addSorted(people, add)
	addSorted(groups, add)

	nameCount := map[string]int{}
	for _, c := range all {
		nameCount[strings.ToLower(c.name)]++
	}

	// rank is 0 when the name starts with the prefix, 1 when a later word
	// does, and -1 when it doesn't match.
	rank := func(c nameCandidate) int {
		if prefix == "" {
			return 0
		}
		if numeric {
			if digits != "" && !strings.Contains(c.key, "@") && strings.HasPrefix(c.key, digits) {
				return 0
			}
			return -1
		}
		name := strings.ToLower(c.name)
		if strings.HasPrefix(name, prefix) {
			return 0
		}
		for _, word := range strings.Fields(name) {
			if strings.HasPrefix(word, prefix) {
				return 1
			}
		}
		return -1
	}

	// Names that start with the prefix first, then names with a matching
	// word; within each, saved contacts keep their place before the rest.
	var ranked [2][]nameCandidate
	for _, c := range all {
		if r := rank(c); r >= 0 {
			ranked[r] = append(ranked[r], c)
		}
	}

	var out []Suggestion
	if prefix == "" || strings.HasPrefix("me", prefix) {
		out = append(out, Suggestion{Word: "me", Description: "yourself"})
	}
	for _, c := range append(ranked[0], ranked[1]...) {
		if limit > 0 && len(out) >= limit {
			break
		}
		group := strings.Contains(c.key, "@")
		switch {
		case numeric && !group:
			out = append(out, Suggestion{Word: "+" + c.key, Description: c.name})
		case nameCount[strings.ToLower(c.name)] > 1 && !group:
			out = append(out, Suggestion{Word: describeChat(c.name, c.key), Description: "contact"})
		case group:
			out = append(out, Suggestion{Word: c.name, Description: groupKind(c.key)})
		default:
			out = append(out, Suggestion{Word: c.name, Description: "+" + c.key})
		}
	}
	return out
}

// phoneKeys keeps only phone-number keys: LID keys point at the same people.
func phoneKeys(names map[string]string) map[string]string {
	out := make(map[string]string, len(names))
	for key, name := range names {
		if !strings.Contains(key, "@") && !strings.ContainsFunc(key, unicode.IsLetter) {
			out[key] = name
		}
	}
	return out
}

func groupKind(key string) string {
	if strings.HasSuffix(key, "@newsletter") {
		return "channel"
	}
	return "group"
}

// matchNames returns the chats whose lowercased name matches, once per chat.
func matchNames(all []nameCandidate, match func(name string) bool) []nameCandidate {
	var out []nameCandidate
	keys := map[string]bool{}
	for _, c := range all {
		if match(strings.ToLower(c.name)) && !keys[c.key] {
			keys[c.key] = true
			out = append(out, c)
		}
	}
	return out
}

// describeChat shows a name with its number, or just the name for groups.
func describeChat(name, key string) string {
	if strings.Contains(key, "@") {
		return name
	}
	return name + " (+" + key + ")"
}
