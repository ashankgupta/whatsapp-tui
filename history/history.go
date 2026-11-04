package history

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Message is a persisted chat message.
type Message struct {
	ID        int64
	ChatJID   string
	SenderJID string
	FromMe    bool
	Text      string
	Timestamp time.Time
	WaID      string
	MediaType string
	MediaPath string
	MediaName string
	MediaRaw  []byte
	Reactions map[string]string
}

// Conversation is the latest message of a chat, used for the chat list.
type Conversation struct {
	ChatJID   string
	Text      string
	Timestamp time.Time
	FromMe    bool
	MediaType string
	MediaName string
}

// Store persists chat history in a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the history database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	schema := `
CREATE TABLE IF NOT EXISTS messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_jid   TEXT NOT NULL,
	sender_jid TEXT NOT NULL DEFAULT '',
	from_me    INTEGER NOT NULL DEFAULT 0,
	text       TEXT NOT NULL,
	timestamp  INTEGER NOT NULL,
	wa_id      TEXT,
	media_type TEXT NOT NULL DEFAULT '',
	media_path TEXT NOT NULL DEFAULT '',
	media_name TEXT NOT NULL DEFAULT '',
	media_raw  BLOB
);
CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(chat_jid, timestamp);
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_wa_id ON messages(wa_id) WHERE wa_id IS NOT NULL AND wa_id != '';
CREATE TABLE IF NOT EXISTS reactions (
	chat_jid     TEXT NOT NULL,
	target_wa_id TEXT NOT NULL,
	sender_jid   TEXT NOT NULL,
	emoji        TEXT NOT NULL,
	timestamp    INTEGER NOT NULL,
	PRIMARY KEY (target_wa_id, sender_jid)
);
CREATE INDEX IF NOT EXISTS idx_reactions_chat ON reactions(chat_jid);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	// Older databases predate media support; add the columns in place.
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{"media_type", "TEXT NOT NULL DEFAULT ''"},
		{"media_path", "TEXT NOT NULL DEFAULT ''"},
		{"media_name", "TEXT NOT NULL DEFAULT ''"},
		{"media_raw", "BLOB"},
	} {
		if err := ensureColumn(db, "messages", column.name, column.ddl); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &Store{db: db}, nil
}

// ensureColumn adds a column to a table when it does not exist yet.
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}

	found := false
	for rows.Next() {
		var (
			cid        int
			name       string
			typ        string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultV, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if found {
		return nil
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Save inserts a message, ignoring duplicates by WhatsApp message ID. It
// reports whether a new row was actually inserted.
func (s *Store) Save(m Message) (bool, error) {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}

	var waID interface{}
	if m.WaID != "" {
		waID = m.WaID
	}

	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages
		 (chat_jid, sender_jid, from_me, text, timestamp, wa_id, media_type, media_path, media_name, media_raw)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ChatJID, m.SenderJID, boolToInt(m.FromMe), m.Text, m.Timestamp.Unix(), waID,
		m.MediaType, m.MediaPath, m.MediaName, m.MediaRaw,
	)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Messages returns up to limit of the most recent messages for a chat in
// chronological order.
func (s *Store) Messages(chatJID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 500
	}

	rows, err := s.db.Query(
		`SELECT id, chat_jid, sender_jid, from_me, text, timestamp, COALESCE(wa_id, ''),
		        COALESCE(media_type, ''), COALESCE(media_path, ''), COALESCE(media_name, ''), media_raw
		 FROM (
			 SELECT * FROM messages
			 WHERE chat_jid = ?
			 ORDER BY timestamp DESC, id DESC
			 LIMIT ?
		 )
		 ORDER BY timestamp ASC, id ASC`,
		chatJID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var (
			m       Message
			fromMe  int
			unixSec int64
		)
		if err := rows.Scan(&m.ID, &m.ChatJID, &m.SenderJID, &fromMe, &m.Text, &unixSec, &m.WaID,
			&m.MediaType, &m.MediaPath, &m.MediaName, &m.MediaRaw); err != nil {
			return nil, err
		}
		m.FromMe = fromMe != 0
		m.Timestamp = time.Unix(unixSec, 0)
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	reactions, err := s.Reactions(chatJID)
	if err != nil {
		return messages, nil
	}
	for i := range messages {
		if r, ok := reactions[messages[i].WaID]; ok {
			messages[i].Reactions = r
		}
	}
	return messages, nil
}

// SaveReaction stores or replaces a reaction on a message. An empty emoji
// removes the reaction.
func (s *Store) SaveReaction(chatJID, targetWaID, senderJID, emoji string, ts time.Time) error {
	if targetWaID == "" || senderJID == "" {
		return fmt.Errorf("reaction is missing target or sender")
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	if emoji == "" {
		_, err := s.db.Exec(
			`DELETE FROM reactions WHERE target_wa_id = ? AND sender_jid = ?`,
			targetWaID, senderJID,
		)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO reactions (chat_jid, target_wa_id, sender_jid, emoji, timestamp)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(target_wa_id, sender_jid) DO UPDATE SET
			 chat_jid = excluded.chat_jid,
			 emoji = excluded.emoji,
			 timestamp = excluded.timestamp`,
		chatJID, targetWaID, senderJID, emoji, ts.Unix(),
	)
	return err
}

// Reactions returns reactions for a chat keyed by target message ID and then
// by the reacting sender's JID.
func (s *Store) Reactions(chatJID string) (map[string]map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT target_wa_id, sender_jid, emoji FROM reactions WHERE chat_jid = ?`,
		chatJID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]map[string]string)
	for rows.Next() {
		var target, sender, emoji string
		if err := rows.Scan(&target, &sender, &emoji); err != nil {
			return nil, err
		}
		if out[target] == nil {
			out[target] = make(map[string]string)
		}
		out[target][sender] = emoji
	}
	return out, rows.Err()
}

// Conversations returns the latest message for every chat, newest first.
func (s *Store) Conversations() ([]Conversation, error) {
	rows, err := s.db.Query(
		`SELECT m.chat_jid, m.text, m.timestamp, m.from_me,
		        COALESCE(m.media_type, ''), COALESCE(m.media_name, '')
		 FROM messages m
		 JOIN (
			 SELECT chat_jid, MAX(id) AS max_id
			 FROM messages
			 GROUP BY chat_jid
		 ) latest ON m.id = latest.max_id
		 ORDER BY m.timestamp DESC, m.id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []Conversation
	for rows.Next() {
		var (
			c       Conversation
			fromMe  int
			unixSec int64
		)
		if err := rows.Scan(&c.ChatJID, &c.Text, &unixSec, &fromMe, &c.MediaType, &c.MediaName); err != nil {
			return nil, err
		}
		c.FromMe = fromMe != 0
		c.Timestamp = time.Unix(unixSec, 0)
		conversations = append(conversations, c)
	}
	return conversations, rows.Err()
}

// SetMediaPath updates the locally cached media metadata for a message.
func (s *Store) SetMediaPath(waID, mediaType, mediaPath, mediaName string) error {
	if waID == "" {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE messages SET media_type = ?, media_path = ?, media_name = ? WHERE wa_id = ?`,
		mediaType, mediaPath, mediaName, waID,
	)
	return err
}

// MediaPath returns the cached file path for a message, if any.
func (s *Store) MediaPath(waID string) (string, error) {
	if waID == "" {
		return "", nil
	}
	var path string
	err := s.db.QueryRow(
		`SELECT COALESCE(media_path, '') FROM messages WHERE wa_id = ? LIMIT 1`,
		waID,
	).Scan(&path)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return path, err
}

// DeleteChat removes all stored messages and reactions for a chat.
func (s *Store) DeleteChat(chatJID string) error {
	if _, err := s.db.Exec(`DELETE FROM reactions WHERE chat_jid = ?`, chatJID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM messages WHERE chat_jid = ?`, chatJID)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// String is a small helper for debugging.
func (m Message) String() string {
	direction := "in"
	if m.FromMe {
		direction = "out"
	}
	return fmt.Sprintf("[%s] %s -> %s: %s", m.Timestamp.Format("15:04"), direction, m.ChatJID, m.Text)
}
