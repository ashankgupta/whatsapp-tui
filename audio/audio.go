// Package audio records and plays WhatsApp voice messages with ffmpeg.
//
// Recording captures the default microphone and encodes it as OGG/Opus, the
// format WhatsApp uses for voice notes. Playback uses ffplay (shipped with
// ffmpeg) or mpv. Both are optional: without them the rest of the app works.
package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MIME is the type WhatsApp expects for voice notes.
const MIME = "audio/ogg; codecs=opus"

// MaxDuration caps a recording, like WhatsApp does.
const MaxDuration = 15 * time.Minute

// ErrNoFFmpeg is returned when ffmpeg is not installed.
var ErrNoFFmpeg = errors.New("ffmpeg is not installed")

// InstallHint tells the user how to get ffmpeg on their system.
func InstallHint() string {
	switch runtime.GOOS {
	case "windows":
		return "install ffmpeg (winget install ffmpeg) and restart the app"
	case "darwin":
		return "install ffmpeg (brew install ffmpeg)"
	default:
		return "install ffmpeg with your package manager (e.g. sudo pacman -S ffmpeg or sudo apt install ffmpeg)"
	}
}

// Recorder is a running microphone recording.
type Recorder struct {
	Path    string
	Started time.Time

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tailBuffer
	done   chan struct{}
	err    error
}

// Record starts recording the default microphone into path as uncompressed
// WAV. Call Stop and then Encode to turn it into a voice note.
func Record(path string) (*Recorder, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, ErrNoFFmpeg
	}
	input, err := inputArgs(ffmpeg)
	if err != nil {
		return nil, err
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, input...)
	args = append(args, "-t", fmt.Sprint(int(MaxDuration.Seconds())), "-ac", "1", "-ar", "48000",
		"-c:a", "pcm_s16le", "-f", "wav", path)

	cmd := exec.Command(ffmpeg, args...)
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	r := &Recorder{Path: path, cmd: cmd, stdin: stdin, stderr: &tailBuffer{}, done: make(chan struct{})}
	cmd.Stderr = r.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start ffmpeg: %w", err)
	}
	r.Started = time.Now()
	go func() {
		r.err = cmd.Wait()
		close(r.done)
	}()
	return r, nil
}

// Elapsed is how long the recording has been running.
func (r *Recorder) Elapsed() time.Duration {
	return time.Since(r.Started)
}

// Failed reports an error if ffmpeg stopped on its own, for example because
// there is no microphone.
func (r *Recorder) Failed() error {
	select {
	case <-r.done:
		return r.failure()
	default:
		return nil
	}
}

func (r *Recorder) failure() error {
	if msg := r.stderr.lastLine(); msg != "" {
		return fmt.Errorf("microphone recording failed: %s", msg)
	}
	if r.err != nil {
		return fmt.Errorf("microphone recording failed: %w", r.err)
	}
	return nil
}

// Stop finishes the recording and waits for the file to be written.
func (r *Recorder) Stop() error {
	select {
	case <-r.done:
		// ffmpeg already exited: hit the length limit or failed.
		if err := r.failure(); err != nil && r.err != nil {
			return err
		}
		return nil
	default:
	}
	// "q" asks ffmpeg to finish the file cleanly.
	_, _ = io.WriteString(r.stdin, "q")
	_ = r.stdin.Close()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		_ = r.cmd.Process.Kill()
		<-r.done
	}
	if _, err := os.Stat(r.Path); err != nil {
		if fail := r.failure(); fail != nil {
			return fail
		}
		return fmt.Errorf("recording was not saved: %w", err)
	}
	return nil
}

// Cancel stops the recording and deletes the file.
func (r *Recorder) Cancel() {
	select {
	case <-r.done:
	default:
		_ = r.cmd.Process.Kill()
		<-r.done
	}
	_ = os.Remove(r.Path)
}

// inputArgs picks the ffmpeg input for the default microphone on this OS.
func inputArgs(ffmpeg string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return []string{"-f", "avfoundation", "-i", ":0"}, nil
	case "windows":
		device, err := windowsMicrophone(ffmpeg)
		if err != nil {
			return nil, err
		}
		return []string{"-f", "dshow", "-i", "audio=" + device}, nil
	default:
		// PulseAudio, which PipeWire also provides, follows the system's
		// default microphone. Fall back to ALSA when ffmpeg lacks it.
		if hasDevice(ffmpeg, "pulse") {
			return []string{"-f", "pulse", "-i", "default"}, nil
		}
		return []string{"-f", "alsa", "-i", "default"}, nil
	}
}

// hasDevice reports whether ffmpeg was built with an input device.
func hasDevice(ffmpeg, name string) bool {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-devices")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name && strings.Contains(fields[0], "D") {
			return true
		}
	}
	return false
}

// Encode cleans up a raw recording and saves it as an OGG/Opus voice note.
//
// Microphones (headsets especially) record far quieter than phones, which
// boost voice notes automatically. The whole recording is processed at once
// so nothing at the end is lost to filter delay.
func Encode(wavPath, oggPath string) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ErrNoFFmpeg
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", wavPath}
	if filter := voiceFilter(ffmpeg); filter != "" {
		args = append(args, "-af", filter)
	}
	args = append(args, "-ac", "1", "-ar", "48000")
	args = append(args, encoderArgs(ffmpeg)...)
	args = append(args, "-f", "ogg", oggPath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	hideWindow(cmd)
	var stderr tailBuffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := stderr.lastLine(); msg != "" {
			return fmt.Errorf("could not encode voice message: %s", msg)
		}
		return fmt.Errorf("could not encode voice message: %w", err)
	}
	return nil
}

// voiceFilter cleans up the microphone signal the way phones do for voice
// notes: cut low rumble, reduce steady background noise, then bring speech
// to the loudness of a typical voice note (-16 LUFS) without clipping.
// Filters missing from older ffmpeg builds are skipped.
func voiceFilter(ffmpeg string) string {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-filters")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	available := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			available[fields[1]] = true
		}
	}
	var chain []string
	for _, f := range []struct{ name, args string }{
		{"highpass", "f=80"},
		{"afftdn", "nf=-25"},
		{"loudnorm", "I=-16:TP=-1.5:LRA=11"},
	} {
		if available[f.name] {
			chain = append(chain, f.name+"="+f.args)
		}
	}
	return strings.Join(chain, ",")
}

// encoderArgs prefers libopus and falls back to ffmpeg's built-in encoder.
func encoderArgs(ffmpeg string) []string {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-encoders")
	hideWindow(cmd)
	if out, err := cmd.Output(); err == nil && strings.Contains(string(out), "libopus") {
		return []string{"-c:a", "libopus", "-b:a", "32k", "-application", "voip"}
	}
	return []string{"-c:a", "opus", "-strict", "-2", "-b:a", "32k"}
}

var dshowDevice = regexp.MustCompile(`"([^"]+)"\s*\(audio\)`)

// windowsMicrophone finds the first DirectShow audio input device.
func windowsMicrophone(ffmpeg string) (string, error) {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput() // always exits with an error
	text := string(out)
	if m := dshowDevice.FindStringSubmatch(text); m != nil {
		return m[1], nil
	}
	// Older ffmpeg lists audio devices in their own section.
	if _, section, ok := strings.Cut(text, "DirectShow audio devices"); ok {
		for _, line := range strings.Split(section, "\n") {
			if strings.Contains(line, "Alternative name") {
				continue
			}
			if _, rest, ok := strings.Cut(line, `"`); ok {
				if name, _, ok := strings.Cut(rest, `"`); ok && name != "" {
					return name, nil
				}
			}
		}
	}
	return "", errors.New("no microphone found")
}

// Analyze returns a recording's length and a 64-bar waveform (0-100), the
// shape WhatsApp draws on voice notes.
func Analyze(path string) (time.Duration, []byte, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return 0, nil, ErrNoFFmpeg
	}
	const rate = 8000
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", path, "-f", "s16le", "-ac", "1", "-ar", fmt.Sprint(rate), "-")
	hideWindow(cmd)
	pcm, err := cmd.Output()
	if err != nil {
		return 0, nil, fmt.Errorf("could not read recording: %w", err)
	}
	samples := len(pcm) / 2
	duration := time.Duration(samples) * time.Second / rate
	return duration, waveform(pcm, 64), nil
}

// waveform averages the loudness of 16-bit PCM into bars scaled to 0-100.
func waveform(pcm []byte, bars int) []byte {
	samples := len(pcm) / 2
	out := make([]byte, bars)
	if samples == 0 {
		return out
	}
	levels := make([]float64, bars)
	peak := 0.0
	for i := range bars {
		start, end := i*samples/bars, (i+1)*samples/bars
		if end <= start {
			continue
		}
		sum := 0.0
		for s := start; s < end; s++ {
			v := int16(binary.LittleEndian.Uint16(pcm[s*2:]))
			if v < 0 {
				sum -= float64(v)
			} else {
				sum += float64(v)
			}
		}
		levels[i] = sum / float64(end-start)
		peak = max(peak, levels[i])
	}
	if peak == 0 {
		return out
	}
	for i, level := range levels {
		out[i] = byte(level / peak * 100)
	}
	return out
}

// Player is a voice message being played.
type Player struct {
	Started time.Time
	cmd     *exec.Cmd
	done    chan struct{}
	once    sync.Once
}

// Play starts playing an audio file with ffplay or mpv.
// Level makes a copy of an audio file at normal voice-note loudness, so quiet
// recordings are easy to hear. The caller deletes the copy.
func Level(path string) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ErrNoFFmpeg
	}
	out, err := os.CreateTemp("", "whatsapp-tui-play-*.wav")
	if err != nil {
		return "", err
	}
	out.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", path,
		"-af", "loudnorm=I=-16:TP=-1.5:LRA=11", "-ac", "1", "-ar", "48000", "-c:a", "pcm_s16le", out.Name())
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		os.Remove(out.Name())
		return "", fmt.Errorf("could not prepare audio: %w", err)
	}
	return out.Name(), nil
}

// Play starts playing an audio file with ffplay or mpv. With removeAfter,
// the file is deleted when playback ends.
func Play(path string, removeAfter bool) (*Player, error) {
	var cmd *exec.Cmd
	if ffplay, err := exec.LookPath("ffplay"); err == nil {
		cmd = exec.Command(ffplay, "-nodisp", "-autoexit", "-loglevel", "quiet", path)
	} else if mpv, err := exec.LookPath("mpv"); err == nil {
		cmd = exec.Command(mpv, "--no-video", "--really-quiet", "--no-terminal", path)
	} else {
		return nil, errors.New("no audio player found; " + InstallHint() + " (it includes ffplay) or install mpv")
	}
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		if removeAfter {
			os.Remove(path)
		}
		return nil, fmt.Errorf("could not start audio player: %w", err)
	}
	p := &Player{Started: time.Now(), cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		if removeAfter {
			os.Remove(path)
		}
		close(p.done)
	}()
	return p, nil
}

// PlayVideo opens a video in its own window with ffplay or mpv. A terminal
// can only show still images, so videos can't play inside the app.
func PlayVideo(path string) (*Player, error) {
	var cmd *exec.Cmd
	if ffplay, err := exec.LookPath("ffplay"); err == nil {
		cmd = exec.Command(ffplay, "-autoexit", "-loglevel", "quiet", "-window_title", "WhatsApp video", path)
	} else if mpv, err := exec.LookPath("mpv"); err == nil {
		cmd = exec.Command(mpv, "--really-quiet", "--no-terminal", "--force-window", "--title=WhatsApp video", path)
	} else {
		return nil, errors.New("no video player found; " + InstallHint() + " (it includes ffplay) or install mpv")
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start video player: %w", err)
	}
	p := &Player{Started: time.Now(), cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// Wait blocks until playback ends.
func (p *Player) Wait() { <-p.done }

// Stop ends playback early.
func (p *Player) Stop() {
	p.once.Do(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
}

// tailBuffer keeps the last few KB of ffmpeg's error output.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > 4096 {
		data := t.buf.Bytes()[t.buf.Len()-4096:]
		t.buf.Reset()
		t.buf.Write(data)
	}
	return len(p), nil
}

func (t *tailBuffer) lastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(t.buf.String()), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
