package whatsapp

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestDescribe(t *testing.T) {
	tests := []struct {
		name    string
		msg     *waE2E.Message
		visible bool
		want    string
	}{
		{"text", &waE2E.Message{Conversation: proto.String("hi")}, true, "hi"},
		{"key exchange only", &waE2E.Message{
			SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String("g")},
			MessageContextInfo:           &waE2E.MessageContextInfo{},
		}, false, ""},
		{"poll vote", &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{}}, false, ""},
		{"empty", &waE2E.Message{}, false, ""},
		{"location", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
			Name: proto.String("Cafe"), DegreesLatitude: proto.Float64(28.6), DegreesLongitude: proto.Float64(77.2),
		}}, true, "📍 Cafe"},
		{"poll", &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{
			Name:    proto.String("Lunch?"),
			Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Yes")}, {OptionName: proto.String("No")}},
		}}, true, "📊 Lunch?\n○ Yes\n○ No"},
		{"contact", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Bob")}}, true, "👤 Contact: Bob"},
		{"voice note", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), Seconds: proto.Uint32(75)}}, true, "🎤 Voice message · 1:15"},
		{"photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, true, "📷 Photo"},
		{"unknown visible", &waE2E.Message{RequestPhoneNumberMessage: &waE2E.RequestPhoneNumberMessage{}}, true, "💬 Message not shown here"},
	}
	for _, tt := range tests {
		c, ok := describe(tt.msg)
		if ok != tt.visible {
			t.Errorf("%s: visible = %v, want %v", tt.name, ok, tt.visible)
			continue
		}
		if !strings.HasPrefix(c.text, tt.want) {
			t.Errorf("%s: text = %q, want prefix %q", tt.name, c.text, tt.want)
		}
	}
}

func TestQuoteOf(t *testing.T) {
	reply := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("yes"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("ABC123"),
			Participant:   proto.String("919876543210:7@s.whatsapp.net"),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("lunch?")},
		},
	}}
	id, text, sender := quoteOf(reply)
	if id != "ABC123" || text != "lunch?" || sender != "919876543210" {
		t.Errorf("quoteOf = %q, %q, %q", id, text, sender)
	}

	photo := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ContextInfo: &waE2E.ContextInfo{
		StanzaID:      proto.String("IMG1"),
		QuotedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}},
	}}}
	if id, text, _ := quoteOf(photo); id != "IMG1" || text != "📷 Photo" {
		t.Errorf("image reply: %q, %q", id, text)
	}

	if id, _, _ := quoteOf(&waE2E.Message{Conversation: proto.String("hi")}); id != "" {
		t.Errorf("plain message reported a quote: %q", id)
	}
}
