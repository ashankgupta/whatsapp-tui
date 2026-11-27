package whatsapp

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// content is what a message shows in the chat.
type content struct {
	text      string
	mediaType string
	mediaName string
	media     bool // the message has downloadable media
}

// hiddenFields are parts of a message that WhatsApp never shows as a bubble:
// encryption key exchanges, poll votes, pins, and the metadata that rides
// along with real messages. A message made only of these is skipped.
var hiddenFields = map[protoreflect.Name]bool{
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
	"messageContextInfo":                         true,
	"protocolMessage":                            true,
	"pollUpdateMessage":                          true,
	"encReactionMessage":                         true,
	"keepInChatMessage":                          true,
	"pinInChatMessage":                           true,
	"encEventResponseMessage":                    true,
	"encCommentMessage":                          true,
	"placeholderMessage":                         true,
	"albumMessage":                               true,
	"stickerSyncRmrMessage":                      true,
	"messageHistoryBundle":                       true,
	"messageHistoryNotice":                       true,
	"groupRootKeyShare":                          true,
	"rootSecretDistributeMessage":                true,
	"secretEncryptedMessage":                     true,
	"pollAddOptionMessage":                       true,
	"statusLinkPreviewMetadata":                  true,
	"call":                                       true,
	"chat":                                       true,
}

// describe turns a message into the text shown in its bubble. ok is false for
// messages that have nothing to show.
func describe(m *waE2E.Message) (c content, ok bool) {
	if m == nil {
		return c, false
	}
	if text := m.GetConversation(); text != "" {
		return content{text: text}, true
	}
	if text := m.GetExtendedTextMessage().GetText(); text != "" {
		return content{text: text}, true
	}

	switch {
	case m.GetImageMessage() != nil:
		return media("image", m.GetImageMessage().GetCaption(), "📷 Photo"), true
	case m.GetVideoMessage() != nil:
		return media("video", m.GetVideoMessage().GetCaption(), "🎥 Video"), true
	case m.GetPtvMessage() != nil:
		return media("video", "", "🎥 Video message"), true
	case m.GetAudioMessage() != nil:
		label := "🎵 Audio"
		if m.GetAudioMessage().GetPTT() {
			label = "🎤 Voice message"
		}
		if secs := m.GetAudioMessage().GetSeconds(); secs > 0 {
			label += fmt.Sprintf(" · %d:%02d", secs/60, secs%60)
		}
		return media("audio", "", label), true
	case m.GetDocumentMessage() != nil:
		doc := m.GetDocumentMessage()
		label := "📄 Document"
		if doc.GetFileName() != "" {
			label = "📄 " + doc.GetFileName()
		}
		c := media("document", doc.GetCaption(), label)
		c.mediaName = doc.GetFileName()
		return c, true
	case m.GetStickerMessage() != nil:
		return media("sticker", "", "🧩 Sticker"), true

	case m.GetLocationMessage() != nil:
		loc := m.GetLocationMessage()
		return content{text: joinLines(
			"📍 "+firstNonEmpty(loc.GetName(), "Location"),
			loc.GetAddress(),
			mapsLink(loc.GetDegreesLatitude(), loc.GetDegreesLongitude()),
		)}, true
	case m.GetLiveLocationMessage() != nil:
		loc := m.GetLiveLocationMessage()
		return content{text: joinLines(
			"📍 Live location",
			loc.GetCaption(),
			mapsLink(loc.GetDegreesLatitude(), loc.GetDegreesLongitude()),
		)}, true
	case m.GetContactMessage() != nil:
		return content{text: "👤 Contact: " + firstNonEmpty(m.GetContactMessage().GetDisplayName(), "shared contact")}, true
	case m.GetContactsArrayMessage() != nil:
		arr := m.GetContactsArrayMessage()
		names := make([]string, 0, len(arr.GetContacts()))
		for _, contact := range arr.GetContacts() {
			if name := contact.GetDisplayName(); name != "" {
				names = append(names, name)
			}
		}
		return content{text: fmt.Sprintf("👤 %d contacts: %s", len(arr.GetContacts()), strings.Join(names, ", "))}, true
	case poll(m) != nil:
		p := poll(m)
		lines := []string{"📊 " + firstNonEmpty(p.GetName(), "Poll")}
		for _, option := range p.GetOptions() {
			lines = append(lines, "○ "+option.GetOptionName())
		}
		return content{text: strings.Join(lines, "\n")}, true
	case m.GetEventMessage() != nil:
		ev := m.GetEventMessage()
		title := "📅 " + firstNonEmpty(ev.GetName(), "Event")
		if ev.GetIsCanceled() {
			title += " (cancelled)"
		}
		return content{text: joinLines(title, ev.GetDescription(), ev.GetLocation().GetName())}, true
	case m.GetGroupInviteMessage() != nil:
		inv := m.GetGroupInviteMessage()
		return content{text: joinLines("👥 Group invite: "+firstNonEmpty(inv.GetGroupName(), "group"), inv.GetCaption())}, true
	case m.GetCallLogMesssage() != nil:
		if m.GetCallLogMesssage().GetIsVideo() {
			return content{text: "📹 Video call"}, true
		}
		return content{text: "📞 Voice call"}, true
	case m.GetScheduledCallCreationMessage() != nil:
		return content{text: "📞 Scheduled call: " + m.GetScheduledCallCreationMessage().GetTitle()}, true

	// Business messages with buttons and menus.
	case m.GetButtonsMessage() != nil:
		return textOr(m.GetButtonsMessage().GetContentText(), "💬 Message with buttons"), true
	case m.GetTemplateMessage() != nil:
		t := m.GetTemplateMessage()
		return textOr(firstNonEmpty(
			t.GetHydratedTemplate().GetHydratedContentText(),
			t.GetHydratedFourRowTemplate().GetHydratedContentText(),
		), "💬 Business message"), true
	case m.GetListMessage() != nil:
		l := m.GetListMessage()
		return textOr(joinLines(l.GetTitle(), l.GetDescription()), "💬 List message"), true
	case m.GetInteractiveMessage() != nil:
		return textOr(m.GetInteractiveMessage().GetBody().GetText(), "💬 Interactive message"), true
	case m.GetButtonsResponseMessage() != nil:
		return textOr(m.GetButtonsResponseMessage().GetSelectedDisplayText(), "💬 Button reply"), true
	case m.GetTemplateButtonReplyMessage() != nil:
		return textOr(m.GetTemplateButtonReplyMessage().GetSelectedDisplayText(), "💬 Button reply"), true
	case m.GetListResponseMessage() != nil:
		return textOr(m.GetListResponseMessage().GetTitle(), "💬 List reply"), true
	case m.GetInteractiveResponseMessage() != nil:
		return textOr(m.GetInteractiveResponseMessage().GetBody().GetText(), "💬 Reply"), true
	case m.GetProductMessage() != nil:
		return textOr(m.GetProductMessage().GetProduct().GetTitle(), "🛍️ Product"), true
	case m.GetOrderMessage() != nil:
		return content{text: "🛒 Order"}, true
	case m.GetSendPaymentMessage() != nil, m.GetRequestPaymentMessage() != nil, m.GetPaymentInviteMessage() != nil:
		return content{text: "💳 Payment"}, true
	}

	// Anything else that only carries hidden parts is skipped; real messages
	// of a kind we don't render yet get a short label instead.
	fields := setFields(m)
	if len(fields) == 0 {
		return c, false
	}
	return content{text: "💬 Message not shown here — open WhatsApp on your phone to see it"}, true
}

// setFields lists the visible parts a message carries.
func setFields(m *waE2E.Message) []string {
	var fields []string
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if !hiddenFields[fd.Name()] {
			fields = append(fields, string(fd.Name()))
		}
		return true
	})
	return fields
}

func poll(m *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{
		m.GetPollCreationMessage(), m.GetPollCreationMessageV2(), m.GetPollCreationMessageV3(),
		m.GetPollCreationMessageV5(), m.GetPollCreationMessageV6(),
	} {
		if p != nil {
			return p
		}
	}
	return nil
}

// editedText is the new text of an edited message.
func editedText(m *waE2E.Message) string {
	c, ok := describe(m)
	if !ok || c.media {
		return firstNonEmpty(
			m.GetImageMessage().GetCaption(),
			m.GetVideoMessage().GetCaption(),
			m.GetDocumentMessage().GetCaption(),
		)
	}
	return c.text
}

func media(kind, caption, label string) content {
	return content{text: firstNonEmpty(caption, label), mediaType: kind, media: true}
}

func textOr(text, label string) content {
	return content{text: firstNonEmpty(text, label)}
}

func mapsLink(lat, lng float64) string {
	if lat == 0 && lng == 0 {
		return ""
	}
	return fmt.Sprintf("https://maps.google.com/?q=%.6f,%.6f", lat, lng)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func joinLines(lines ...string) string {
	var out []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// quoteOf returns the message a reply quotes: its ID, the text WhatsApp shows
// for it and the phone number (or JID) of its sender. All are empty when the
// message is not a reply.
func quoteOf(m *waE2E.Message) (id, text, sender string) {
	var info *waE2E.ContextInfo
	switch {
	case m.GetExtendedTextMessage() != nil:
		info = m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		info = m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		info = m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		info = m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		info = m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		info = m.GetStickerMessage().GetContextInfo()
	case m.GetLocationMessage() != nil:
		info = m.GetLocationMessage().GetContextInfo()
	case m.GetContactMessage() != nil:
		info = m.GetContactMessage().GetContextInfo()
	}
	id = info.GetStanzaID()
	if id == "" {
		return "", "", ""
	}
	if c, ok := describe(info.GetQuotedMessage()); ok {
		text = c.text
	}
	sender = info.GetParticipant()
	if user, _, found := strings.Cut(sender, "@"); found {
		// Drop the device part ("123:4@s.whatsapp.net") but keep other servers' JIDs intact.
		if strings.HasSuffix(sender, "@s.whatsapp.net") {
			sender, _, _ = strings.Cut(user, ":")
		}
	}
	return id, text, sender
}
