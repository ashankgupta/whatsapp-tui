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
