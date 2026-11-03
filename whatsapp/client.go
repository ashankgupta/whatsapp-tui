package whatsapp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// IncomingMessage is a simplified representation of a WhatsApp message event.
type IncomingMessage struct {
	Sender     string
	PushName   string
	Chat       string
	Text       string
	Time       time.Time
	IsGroup    bool
	FromMe     bool
	ID         string
	IsReaction bool
	TargetID   string
	MediaType  string
	MediaName  string
	Raw        *waE2E.Message
}

// CallEvent is a simplified representation of a WhatsApp call event.
type CallEvent struct {
	Type     string // "offer", "end", or "answered"
	CallID   string
	From     string
	Caller   string
	IsGroup  bool
	IsVideo  bool
	GroupJID string
	Time     time.Time
}

// chatKey returns the identifier used to group messages into one conversation.
// Direct chats use the phone number (e.g. "919876543210"), groups use the full
// group JID (e.g. "1234567890-123456@g.us").
func chatKey(info types.MessageInfo) string {
	if info.IsGroup {
		return info.Chat.String()
	}

	jid := info.Chat
	if info.IsFromMe {
		if jid.Server == types.HiddenUserServer && !info.RecipientAlt.IsEmpty() {
			jid = info.RecipientAlt
		}
	} else if jid.Server == types.HiddenUserServer && !info.SenderAlt.IsEmpty() {
		jid = info.SenderAlt
	}

	if jid.Server == types.DefaultUserServer {
		return jid.User
	}
	return jid.String()
}

// senderKey returns the phone number of the sender when it can be resolved.
func senderKey(info types.MessageInfo) string {
	jid := info.Sender
	if jid.Server == types.HiddenUserServer && !info.SenderAlt.IsEmpty() {
		jid = info.SenderAlt
	}
	return jid.User
}

// Listen registers an event handler for message events and streams them on the
// returned channel.
func Listen(client *whatsmeow.Client) <-chan IncomingMessage {
	ch := make(chan IncomingMessage, 100)
	client.AddEventHandler(func(evt interface{}) {
		msg, ok := evt.(*events.Message)
		if !ok {
			return
		}

		if reaction := msg.Message.GetReactionMessage(); reaction != nil {
			if target := reaction.GetKey().GetID(); target != "" {
				select {
				case ch <- IncomingMessage{
					Sender:     senderKey(msg.Info),
					PushName:   msg.Info.PushName,
					Chat:       chatKey(msg.Info),
					Text:       reaction.GetText(),
					Time:       msg.Info.Timestamp,
					IsGroup:    msg.Info.IsGroup,
					FromMe:     msg.Info.IsFromMe,
					ID:         string(msg.Info.ID),
					IsReaction: true,
					TargetID:   target,
				}:
				default:
				}
			}
			return
		}

		text := msg.Message.GetConversation()
		if text == "" {
			text = msg.Message.GetExtendedTextMessage().GetText()
		}

		mediaType := ""
		mediaName := ""
		var raw *waE2E.Message
		if text == "" {
			switch {
			case msg.Message.GetImageMessage() != nil:
				mediaType = "image"
				raw = msg.Message
				text = msg.Message.GetImageMessage().GetCaption()
				if text == "" {
					text = "📷 Photo"
				}
			case msg.Message.GetVideoMessage() != nil:
				mediaType = "video"
				raw = msg.Message
				text = msg.Message.GetVideoMessage().GetCaption()
				if text == "" {
					text = "🎥 Video"
				}
			case msg.Message.GetAudioMessage() != nil:
				mediaType = "audio"
				raw = msg.Message
				text = "🎵 Audio"
				if msg.Message.GetAudioMessage().GetPTT() {
					text = "🎤 Voice message"
				}
			case msg.Message.GetDocumentMessage() != nil:
				mediaType = "document"
				raw = msg.Message
				mediaName = msg.Message.GetDocumentMessage().GetFileName()
				text = msg.Message.GetDocumentMessage().GetCaption()
				if text == "" {
					if mediaName != "" {
						text = "📄 " + mediaName
					} else {
						text = "📄 Document"
					}
				}
			case msg.Message.GetStickerMessage() != nil:
				mediaType = "sticker"
				raw = msg.Message
				text = "🧩 Sticker"
			default:
				text = "[unsupported message]"
			}
		}

		incoming := IncomingMessage{
			Sender:    senderKey(msg.Info),
			PushName:  msg.Info.PushName,
			Chat:      chatKey(msg.Info),
			Text:      text,
			Time:      msg.Info.Timestamp,
			IsGroup:   msg.Info.IsGroup,
			FromMe:    msg.Info.IsFromMe,
			ID:        string(msg.Info.ID),
			MediaType: mediaType,
			MediaName: mediaName,
			Raw:       raw,
		}

		select {
		case ch <- incoming:
		default:
		}
	})
	return ch
}

// ListenCalls registers an event handler for incoming call events and streams
// them on the returned channel.
func ListenCalls(client *whatsmeow.Client) <-chan CallEvent {
	ch := make(chan CallEvent, 10)
	client.AddEventHandler(func(evt interface{}) {
		switch evt := evt.(type) {
		case *events.CallOffer:
			caller := evt.CallCreatorAlt
			if caller.IsEmpty() {
				caller = evt.CallCreator
			}
			isVideo := false
			if evt.Data != nil {
				if evt.Data.AttrGetter().String("media") == "video" {
					isVideo = true
				}
				for _, child := range evt.Data.GetChildren() {
					if child.Tag == "video" {
						isVideo = true
					}
				}
			}
			select {
			case ch <- CallEvent{
				Type:     "offer",
				CallID:   evt.CallID,
				From:     evt.From.String(),
				Caller:   caller.User,
				IsGroup:  !evt.GroupJID.IsEmpty(),
				IsVideo:  isVideo,
				GroupJID: evt.GroupJID.String(),
				Time:     evt.Timestamp,
			}:
			default:
			}

		case *events.CallOfferNotice:
			caller := evt.CallCreatorAlt
			if caller.IsEmpty() {
				caller = evt.CallCreator
			}
			from := evt.From.String()
			if from == "" {
				from = evt.GroupJID.String()
			}
			select {
			case ch <- CallEvent{
				Type:     "offer",
				CallID:   evt.CallID,
				From:     from,
				Caller:   caller.User,
				IsGroup:  true,
				IsVideo:  evt.Media == "video",
				GroupJID: evt.GroupJID.String(),
				Time:     evt.Timestamp,
			}:
			default:
			}

		case *events.CallTerminate:
			select {
			case ch <- CallEvent{Type: "end", CallID: evt.CallID, Time: evt.Timestamp}:
			default:
			}

		case *events.CallReject:
			select {
			case ch <- CallEvent{Type: "end", CallID: evt.CallID, Time: evt.Timestamp}:
			default:
			}

		case *events.CallAccept:
			select {
			case ch <- CallEvent{Type: "answered", CallID: evt.CallID, Time: evt.Timestamp}:
			default:
			}
		}
	})
	return ch
}

var (
	logOnce sync.Once
	logOut  io.Writer = io.Discard
)

// waLogger writes whatsmeow diagnostics to whatsapp.log so they do not corrupt
// the terminal UI. If the file cannot be opened, logs are discarded.
func waLogger(module string) waLog.Logger {
	logOnce.Do(func() {
		file, err := os.OpenFile("whatsapp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			logOut = file
		}
	})
	return waLog.Zerolog(zerolog.New(logOut).With().Timestamp().Str("module", module).Logger())
}

// Connect initializes and connects to WhatsApp, returning a client instance.
func Connect(dbPath string) *whatsmeow.Client {
	container, err := sqlstore.New(context.Background(), "sqlite3", dbPath, waLogger("DB"))
	if err != nil {
		log.Fatalf("❌ Database error: %v", err)
	}

	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		log.Fatalf("❌ No device found: %v", err)
	}

	client := whatsmeow.NewClient(device, waLogger("Client"))

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			log.Fatalf("❌ Failed to connect: %v", err)
		}
		timeout := time.After(60 * time.Second)
		for {
			select {
			case evt, ok := <-qrChan:
				if !ok {
					log.Fatalf("❌ QR channel closed unexpectedly")
				}
				if evt.Event == "code" {
					qr, err := qrcode.New(evt.Code, qrcode.Medium)
					if err != nil {
						log.Fatalf("❌ QR code generation failed: %v", err)
					}
					fmt.Println("\n📲 Scan this QR code in your WhatsApp app:")
					fmt.Println(qr.ToSmallString(false))
				} else if evt.Event == "success" {
					fmt.Println("✅ QR code scanned successfully!")
					return client
				}
			case <-timeout:
				log.Fatalf("❌ QR code scanning timed out after 60 seconds")
			}
		}
	} else {
		err := client.Connect()
		if err != nil {
			log.Fatalf("❌ Connection failed: %v", err)
		}
	}

	return client
}
