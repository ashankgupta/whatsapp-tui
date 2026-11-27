package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReplyAndEdit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now()
	if _, err := store.Save(Message{ChatJID: "1", Text: "lunch?", WaID: "A", Timestamp: now}); err != nil {
		t.Fatal(err)
	}
	reply := Message{
		ChatJID: "1", FromMe: true, Text: "yes", WaID: "B", Timestamp: now.Add(time.Second),
		ReplyID: "A", ReplyText: "lunch?", ReplySender: "1",
	}
	if _, err := store.Save(reply); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.EditText("B", "yes please"); err != nil || !ok {
		t.Fatalf("EditText = %v, %v", ok, err)
	}

	msgs, err := store.Messages("1", 10)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("Messages = %d, %v", len(msgs), err)
	}
	if msgs[0].ReplyID != "" || msgs[0].Edited {
		t.Errorf("original should be plain: %+v", msgs[0])
	}
	got := msgs[1]
	if got.ReplyID != "A" || got.ReplyText != "lunch?" || got.ReplySender != "1" || !got.Edited || got.Text != "yes please" {
		t.Errorf("reply lost data: %+v", got)
	}

	if ok, err := store.MarkDeleted("B"); err != nil || !ok {
		t.Fatalf("MarkDeleted = %v, %v", ok, err)
	}
	if ok, _ := store.EditText("B", "late edit"); ok {
		t.Error("a deleted message must not be edited")
	}
}
