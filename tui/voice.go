package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatsapp-tui/audio"
	"whatsapp-tui/history"
)

type (
	// voiceTickMsg redraws the recording timer and playback progress.
	voiceTickMsg struct{}
	// audioFileMsg is delivered when a voice message has been downloaded.
	audioFileMsg struct {
		waID      string
		mediaType string // stored type, kept as is (a video file can be a document)
		kind      string // "audio" or "video": how to play it
		path      string
		err       error
	}
	// playDoneMsg is delivered when playback ends.
	playDoneMsg struct{ player *audio.Player }
	// playReadyMsg is delivered when a voice message has been prepared for
	// playback at normal loudness.
	playReadyMsg struct {
		waID     string
		video    bool
		path     string // file to play
		temp     bool   // path is a leveled copy to delete afterwards
		length   time.Duration
		levelErr error
	}
)

func voiceTick() bubbletea.Cmd {
	return bubbletea.Tick(250*time.Millisecond, func(time.Time) bubbletea.Msg { return voiceTickMsg{} })
}

// formatDuration renders a duration as m:ss.
func formatDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// startRecording begins a voice message in the open chat.
func (m *Model) startRecording() bubbletea.Cmd {
	if m.isSending || m.isUploading {
		m.statusMessage = "Please wait for the current send to finish"
		return nil
	}
	m.stopPlayback()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("whatsapp-tui-voice-%d.wav", time.Now().UnixNano()))
	rec, err := audio.Record(path)
	if errors.Is(err, audio.ErrNoFFmpeg) {
		m.statusMessage = "❌ To record voice messages, " + audio.InstallHint()
		return nil
	}
	if err != nil {
		m.statusMessage = "❌ " + err.Error()
		return nil
	}
	wasBottom := m.chatScroll >= m.maxChatScroll()
	m.recorder = rec
	m.statusMessage = ""
	m.keepChatBottom(wasBottom)
	return voiceTick()
}

// cancelRecording throws the current recording away.
func (m *Model) cancelRecording() {
	if m.recorder == nil {
		return
	}
	m.recorder.Cancel()
	m.recorder = nil
	m.statusMessage = "Voice message discarded"
}

// finishRecording stops the recording and sends it.
func (m *Model) finishRecording() bubbletea.Cmd {
	rec := m.recorder
	m.recorder = nil
	if rec.Elapsed() < time.Second {
		rec.Cancel()
		m.statusMessage = "⚠️ Voice message too short — hold on a bit longer"
		return nil
	}
	m.isUploading = true
	m.statusMessage = "Sending voice message..."
	return m.sendVoiceCmd(rec)
}

// sendVoiceCmd finishes the recording and sends it as a WhatsApp voice note.
func (m Model) sendVoiceCmd(rec *audio.Recorder) bubbletea.Cmd {
	return func() bubbletea.Msg {
		result := fileSendResult{chatJID: m.activeChat}
		voicePath := rec.Path[:len(rec.Path)-len(filepath.Ext(rec.Path))] + ".ogg"
		defer os.Remove(rec.Path)
		defer os.Remove(voicePath)

		if err := rec.Stop(); err != nil {
			result.err = err
			return result
		}
		if err := audio.Encode(rec.Path, voicePath); err != nil {
			result.err = err
			return result
		}
		length, waveform, err := audio.Analyze(voicePath)
		if err != nil {
			result.err = err
			return result
		}
		if m.client == nil || m.activeChat == "" {
			result.err = fmt.Errorf("no chat selected")
			return result
		}
		recipient, err := m.recipientJID()
		if err != nil {
			result.err = err
			return result
		}
		data, err := os.ReadFile(voicePath)
		if err != nil {
			result.err = err
			return result
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		upload, err := m.client.Upload(ctx, data, whatsmeow.MediaAudio)
		if err != nil {
			result.err = err
			return result
		}
		seconds := uint32(max(1, length.Round(time.Second).Seconds()))
		resp, err := m.client.SendMessage(ctx, recipient, &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(upload.URL),
			DirectPath:    proto.String(upload.DirectPath),
			MediaKey:      upload.MediaKey,
			FileEncSHA256: upload.FileEncSHA256,
			FileSHA256:    upload.FileSHA256,
			FileLength:    proto.Uint64(upload.FileLength),
			Mimetype:      proto.String(audio.MIME),
			Seconds:       proto.Uint32(seconds),
			PTT:           proto.Bool(true),
			Waveform:      waveform,
		}})
		if err != nil {
			result.err = err
			return result
		}

		id := string(resp.ID)
		if id == "" {
			id = fmt.Sprintf("out-%d", time.Now().UnixNano())
		}
		cachePath, err := cacheMediaBytes(id, "", audio.MIME, data)
		if err != nil {
			result.warn = "sent, but could not keep a copy: " + err.Error()
		}
		result.text = voiceLabel(seconds)
		result.msgID = id
		result.time = resp.Timestamp
		result.mediaType = "audio"
		result.mediaPath = cachePath
		return result
	}
}

// voiceLabel is the bubble text for a voice message.
func voiceLabel(seconds uint32) string {
	return "🎤 Voice message · " + formatDuration(time.Duration(seconds)*time.Second)
}

// playKind says how to play a message: "audio", "video", or "" when it
// isn't playable. Audio and video files sent as documents count too.
func playKind(msg history.Message) string {
	switch msg.MediaType {
	case "audio", "video":
		return msg.MediaType
	case "document":
		name := msg.MediaName
		if name == "" {
			name = msg.MediaPath
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp4", ".mkv", ".mov", ".webm", ".avi", ".m4v", ".3gp", ".wmv", ".flv", ".mpeg", ".mpg":
			return "video"
		case ".mp3", ".ogg", ".opus", ".m4a", ".wav", ".aac", ".flac", ".amr", ".wma":
			return "audio"
		}
	}
	return ""
}

// isPlayable reports whether a message holds audio or video the app can
// play and that is still available.
func isPlayable(msg history.Message) bool {
	return playKind(msg) != "" && (fileExists(msg.MediaPath) || len(msg.MediaRaw) > 0)
}

// mediaNoun names a playable message in status messages.
func mediaNoun(mediaType string) string {
	if mediaType == "video" {
		return "video"
	}
	return "voice message"
}

// startChoosingAudio lets the user pick a voice message to play, starting
// with the newest one.
func (m *Model) startChoosingAudio() {
	for i := len(m.chatMessages) - 1; i >= 0; i-- {
		if isPlayable(m.chatMessages[i]) {
			m.choosingAudio = true
			m.audioTarget = i
			m.audioScroll = m.chatScroll
			m.scrollToMessage(i)
			m.statusMessage = ""
			return
		}
	}
	m.statusMessage = "No voice messages or videos in this chat"
}

// moveAudioTarget selects the previous (-1) or next (+1) playable message.
func (m *Model) moveAudioTarget(step int) {
	for i := m.audioTarget + step; i >= 0 && i < len(m.chatMessages); i += step {
		if isPlayable(m.chatMessages[i]) {
			m.audioTarget = i
			m.scrollToMessage(i)
			return
		}
	}
}

// playSelectedAudio plays the chosen message, downloading it first if needed.
func (m *Model) playSelectedAudio() bubbletea.Cmd {
	m.choosingAudio = false
	m.chatScroll = m.audioScroll
	if m.audioTarget < 0 || m.audioTarget >= len(m.chatMessages) {
		return nil
	}
	msg := m.chatMessages[m.audioTarget]
	kind := playKind(msg)
	if fileExists(msg.MediaPath) {
		return m.playFile(msg.WaID, kind, msg.MediaPath)
	}
	m.stopPlayback()
	m.playingID = msg.WaID
	m.statusMessage = "Downloading " + mediaNoun(kind) + "..."
	return m.downloadAudioCmd(msg)
}

// downloadAudioCmd fetches a voice message or video so it can be played.
func (m Model) downloadAudioCmd(rec history.Message) bubbletea.Cmd {
	return func() bubbletea.Msg {
		result := audioFileMsg{waID: rec.WaID, mediaType: rec.MediaType, kind: playKind(rec)}
		var raw waE2E.Message
		if err := proto.Unmarshal(rec.MediaRaw, &raw); err != nil || m.client == nil {
			result.err = fmt.Errorf("%s is no longer available", mediaNoun(result.kind))
			return result
		}
		// Videos can be large; give them longer to download.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		data, err := m.client.DownloadAny(ctx, &raw)
		if err != nil {
			result.err = err
			return result
		}
		mimeType := rawMediaMIME(&raw)
		if mimeType == "" && result.kind == "video" {
			mimeType = "video/mp4"
		} else if mimeType == "" {
			mimeType = audio.MIME
		}
		result.path, result.err = cacheMediaBytes(rec.WaID, rec.MediaName, mimeType, data)
		return result
	}
}

// handleAudioFile stores a downloaded voice message and plays it.
func (m *Model) handleAudioFile(msg audioFileMsg) bubbletea.Cmd {
	if msg.err != nil {
		if m.playingID == msg.waID {
			m.playingID = ""
		}
		m.statusMessage = "❌ Could not download " + mediaNoun(msg.kind) + ": " + msg.err.Error()
		return nil
	}
	if m.store != nil {
		_ = m.store.SetMediaPath(msg.waID, msg.mediaType, msg.path, m.mediaName(msg.waID))
	}
	for i := range m.chatMessages {
		if m.chatMessages[i].WaID == msg.waID {
			m.chatMessages[i].MediaPath = msg.path
		}
	}
	// Only play it if the user hasn't started something else meanwhile.
	if m.playingID != msg.waID || m.player != nil {
		return nil
	}
	return m.playFile(msg.waID, msg.kind, msg.path)
}

// mediaName is the stored file name of a message in the open chat.
func (m Model) mediaName(waID string) string {
	for _, msg := range m.chatMessages {
		if msg.WaID == waID {
			return msg.MediaName
		}
	}
	return ""
}

// playFile plays a cached voice message or video. Voice messages are first
// leveled to normal loudness; videos open in their own window.
func (m *Model) playFile(waID, mediaType, path string) bubbletea.Cmd {
	m.stopPlayback()
	m.playingID = waID
	m.playingLength = 0
	if mediaType == "video" {
		return m.startPlayer(playReadyMsg{waID: waID, video: true, path: path})
	}
	m.statusMessage = "Loading voice message..."
	return func() bubbletea.Msg {
		ready := playReadyMsg{waID: waID, path: path}
		if leveled, err := audio.Level(path); err == nil {
			ready.path, ready.temp = leveled, true
		} else {
			ready.levelErr = err // play the original instead
		}
		ready.length, _, _ = audio.Analyze(ready.path)
		return ready
	}
}

// startPlayer plays a prepared voice message unless the user moved on.
func (m *Model) startPlayer(msg playReadyMsg) bubbletea.Cmd {
	if m.playingID != msg.waID || m.player != nil {
		if msg.temp {
			os.Remove(msg.path)
		}
		return nil
	}
	var (
		player *audio.Player
		err    error
	)
	if msg.video {
		player, err = audio.PlayVideo(msg.path)
	} else {
		player, err = audio.Play(msg.path, msg.temp)
	}
	if err != nil {
		m.playingID = ""
		m.statusMessage = "❌ " + err.Error()
		return nil
	}
	m.player = player
	m.playingVideo = msg.video
	m.playingLength = msg.length
	m.statusMessage = m.playbackStatus()
	return bubbletea.Batch(voiceTick(), func() bubbletea.Msg {
		player.Wait()
		return playDoneMsg{player: player}
	})
}

// stopPlayback stops any voice message that is playing.
func (m *Model) stopPlayback() {
	if m.player != nil {
		m.player.Stop()
	}
	m.player = nil
	m.playingID = ""
}

// stopAudio ends recording and playback, used when quitting.
func (m *Model) stopAudio() {
	if m.recorder != nil {
		m.recorder.Cancel()
		m.recorder = nil
	}
	m.stopPlayback()
}

func (m Model) playbackStatus() string {
	elapsed := time.Since(m.player.Started)
	progress := formatDuration(elapsed)
	if m.playingLength > 0 {
		elapsed = min(elapsed, m.playingLength)
		progress = formatDuration(elapsed) + " / " + formatDuration(m.playingLength)
	}
	if m.playingVideo {
		return "▶ Playing video in its own window  " + progress + "  •  ^P Stop"
	}
	return "▶ Playing voice message  " + progress + "  •  ^P Stop"
}

// handleVoiceTick keeps the recording timer and playback progress moving.
func (m *Model) handleVoiceTick() bubbletea.Cmd {
	if m.recorder != nil {
		if err := m.recorder.Failed(); err != nil {
			m.recorder.Cancel()
			m.recorder = nil
			m.statusMessage = "❌ " + err.Error()
			return nil
		}
		if m.recorder.Elapsed() >= audio.MaxDuration {
			return m.finishRecording()
		}
		return voiceTick()
	}
	if m.player != nil {
		m.statusMessage = m.playbackStatus()
		return voiceTick()
	}
	return nil
}

// handleVoiceKey handles keys while recording or picking a message to play.
func (m *Model) handleVoiceKey(msg bubbletea.KeyMsg) (bubbletea.Cmd, bool) {
	if m.step != 1 {
		return nil, false
	}
	if m.recorder != nil {
		switch msg.String() {
		case "enter", "ctrl+t":
			return m.finishRecording(), true
		case "esc":
			m.cancelRecording()
		}
		return nil, true
	}
	if m.choosingAudio {
		switch msg.String() {
		case "enter", "ctrl+p":
			return m.playSelectedAudio(), true
		case "up", "k":
			m.moveAudioTarget(-1)
		case "down", "j":
			m.moveAudioTarget(1)
		case "esc":
			m.choosingAudio = false
			m.chatScroll = m.audioScroll
		}
		return nil, true
	}
	if m.attaching || m.reacting || m.savingMedia {
		return nil, false
	}
	switch msg.String() {
	case "ctrl+t":
		return m.startRecording(), true
	case "ctrl+p":
		if m.player != nil || m.playingID != "" {
			m.stopPlayback()
			m.statusMessage = "Playback stopped"
			return nil, true
		}
		m.startChoosingAudio()
		return nil, true
	}
	return nil, false
}

// renderRecording replaces the message box while recording.
func (m Model) renderRecording(innerWidth int) string {
	elapsed := m.recorder.Elapsed()
	dot := "●"
	if int(elapsed/(500*time.Millisecond))%2 == 1 {
		dot = " "
	}
	line := errorTextStyle.Render(dot+" Recording") + "  " + normalStyle.Render(formatDuration(elapsed)) +
		"   " + mutedStyle.Render("Speak now…")
	return inputBarStyle.Width(innerWidth).Render(line)
}
