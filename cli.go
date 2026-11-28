package main

// `whatsapp-tui send`: send messages and files from the shell.
//
// WhatsApp allows one connection per linked device, so the command can't
// connect while the app is open. Instead the open app listens on a local
// socket (controlSocket) and sends on the command's behalf. When the app
// isn't running, the command connects by itself, sends, saves any messages
// that arrived meanwhile to history, and disconnects.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow"

	"whatsapp-tui/contacts"
	"whatsapp-tui/history"
	"whatsapp-tui/tui"
	"whatsapp-tui/whatsapp"
)

// controlSocket is where an open app accepts send requests. It lives next to
// session.db, so it belongs to that account.
const controlSocket = "whatsapp-tui.sock"

const sessionDSN = "file:session.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

const sendUsage = `Send a WhatsApp message or file from the command line.

Usage:
  whatsapp-tui send <to> <message...>
  whatsapp-tui send <to> --file <path> [caption...]
  <command> | whatsapp-tui send <to>

<to> is one of:
  me                     yourself (your own chat, handy for notifications)
  a contact or group     a name from contacts.json, your phone's address book,
                         or a group or channel title; part of a name works
                         when it matches only one chat
  a phone number         with country code, e.g. +91 98765 43210
  a WhatsApp ID          e.g. 1203630000000000@g.us

Options:
  -f, --file <path>   send a file: photos as images, anything else as a document;
                      the message words become its caption
  -h, --help          show this help

The message is read from standard input when no words are given, or when
the message is "-".

If the app is open, it sends for you and the message shows up in the chat.
Otherwise the command connects with the saved session; log in once with the
app first. Run it from the folder with session.db, or use --dir.

Examples:
  whatsapp-tui send me "Build finished"
  whatsapp-tui send Rahul "Running 10 minutes late"
  whatsapp-tui send "Family" --file photo.jpg "From the trip"
  make test 2>&1 | tail -20 | whatsapp-tui send me
`

const mainUsage = `whatsapp-tui: WhatsApp in your terminal.

Usage:
  whatsapp-tui [--dir <folder>]                     open the app
  whatsapp-tui [--dir <folder>] send <to> <message>  send from the shell (see send --help)
  whatsapp-tui completion install                    turn on Tab completion for contacts (see completion --help)
  whatsapp-tui --version
  whatsapp-tui --help

--dir runs in another folder (where session.db, history.db and contacts.json
live) instead of the current one.
`

// sendRequest is what the command asks the open app to send.
type sendRequest struct {
	To      string `json:"to"`
	Text    string `json:"text,omitempty"`
	File    string `json:"file,omitempty"`    // absolute path
	Caption string `json:"caption,omitempty"` // for files
}

// sendResponse is the app's answer.
type sendResponse struct {
	OK    bool   `json:"ok"`
	To    string `json:"to,omitempty"`
	Error string `json:"error,omitempty"`
}

// runSend implements `whatsapp-tui send` and returns the exit code.
func runSend(args []string) int {
	req, err := parseSendArgs(args, os.Stdin)
	if errors.Is(err, errHelp) {
		fmt.Print(sendUsage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "whatsapp-tui send:", err)
		fmt.Fprintln(os.Stderr, "Run 'whatsapp-tui send --help' for usage.")
		return 2
	}

	resp, handled := sendThroughApp(req)
	if !handled {
		resp = sendDirectly(req)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, "❌", resp.Error)
		return 1
	}
	fmt.Println("✅ Sent to", resp.To)
	return 0
}

var errHelp = errors.New("help requested")

// parseSendArgs reads `send` arguments. Options may appear anywhere.
func parseSendArgs(args []string, stdin *os.File) (sendRequest, error) {
	var (
		req   sendRequest
		words []string
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help" || arg == "help":
			return req, errHelp
		case arg == "-f" || arg == "--file":
			if i+1 >= len(args) {
				return req, fmt.Errorf("%s needs a file path", arg)
			}
			i++
			req.File = args[i]
		case strings.HasPrefix(arg, "--file="):
			req.File = strings.TrimPrefix(arg, "--file=")
		case arg == "--":
			words = append(words, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-") && arg != "-" && len(words) == 0 && req.To == "":
			return req, fmt.Errorf("unknown option %s", arg)
		case req.To == "":
			req.To = arg
		default:
			words = append(words, arg)
		}
	}
	if req.To == "" {
		return req, fmt.Errorf("say who to send to")
	}

	text := strings.Join(words, " ")
	if (text == "" && req.File == "") || text == "-" {
		if isTerminal(stdin) {
			return req, fmt.Errorf("no message given")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return req, fmt.Errorf("could not read the message from input: %w", err)
		}
		text = string(data)
	}
	text = strings.TrimSpace(text)

	if req.File != "" {
		abs, err := filepath.Abs(req.File)
		if err != nil {
			return req, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return req, fmt.Errorf("can't send %s: %w", req.File, err)
		}
		if info.IsDir() {
			return req, fmt.Errorf("%s is a folder, not a file", req.File)
		}
		req.File, req.Caption = abs, text
		return req, nil
	}
	if text == "" {
		return req, fmt.Errorf("the message is empty")
	}
	req.Text = text
	return req, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// sendThroughApp hands the request to an open app. handled is false when no
// app is listening.
func sendThroughApp(req sendRequest) (resp sendResponse, handled bool) {
	conn, err := net.DialTimeout("unix", controlSocket, time.Second)
	if err != nil {
		return resp, false
	}
	defer conn.Close()
	// Large files can take a while to upload.
	_ = conn.SetDeadline(time.Now().Add(15 * time.Minute))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return sendResponse{Error: "could not reach the open app: " + err.Error()}, true
	}
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return sendResponse{Error: "the open app didn't answer: " + err.Error()}, true
	}
	return resp, true
}

// sendDirectly connects with the saved session, sends, and disconnects.
func sendDirectly(req sendRequest) sendResponse {
	if _, err := os.Stat("session.db"); err != nil {
		return sendResponse{Error: "no session.db here: run this in the app's folder (or use --dir), after logging in with the app once"}
	}
	saved, err := contacts.LoadContacts(contactsFile)
	if err != nil {
		return sendResponse{Error: "could not load contacts: " + err.Error()}
	}
	store, err := history.Open("history.db")
	if err != nil {
		return sendResponse{Error: "could not open chat history: " + err.Error()}
	}
	defer store.Close()

	client, err := whatsapp.ConnectExisting(sessionDSN, 30*time.Second)
	if err != nil {
		return sendResponse{Error: err.Error()}
	}

	// Messages that arrive while connected are delivered here and not to the
	// app later, so keep them in history.
	saver := startSaver(client, store)
	resp := deliver(client, store, saved, req)

	// Let WhatsApp finish delivering messages that came in while we were
	// offline, then disconnect and save what is still queued.
	saver.waitQuiet(time.Second, 6*time.Second)
	client.Disconnect()
	saver.drain(3 * time.Second)
	return resp
}

// incomingSaver writes messages received during a command-line run to
// history and tracks activity so the run can end once things are quiet.
type incomingSaver struct {
	incoming <-chan whatsapp.IncomingMessage
	mu       sync.Mutex
	last     time.Time // when the last message arrived
	busy     bool      // saving a message right now
}

func startSaver(client *whatsmeow.Client, store *history.Store) *incomingSaver {
	s := &incomingSaver{incoming: whatsapp.Listen(client), last: time.Now()}
	go func() {
		for msg := range s.incoming {
			s.mu.Lock()
			s.last, s.busy = time.Now(), true
			s.mu.Unlock()
			_ = tui.SaveIncoming(store, msg)
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}
	}()
	return s
}

// waitQuiet returns once no message has arrived for quiet, or after max.
func (s *incomingSaver) waitQuiet(quiet, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		idle := time.Since(s.last) >= quiet && !s.busy && len(s.incoming) == 0
		s.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// drain waits until every queued message has been saved.
func (s *incomingSaver) drain(max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := !s.busy && len(s.incoming) == 0
		s.mu.Unlock()
		if done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// deliver resolves the recipient and sends the text or file.
func deliver(client *whatsmeow.Client, store *history.Store, saved []contacts.Contact, req sendRequest) sendResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	chat, display, err := tui.ResolveChat(ctx, client, store, saved, req.To)
	if err != nil {
		return sendResponse{Error: err.Error()}
	}
	if req.File != "" {
		_, err = tui.SendFile(ctx, client, store, chat, req.File, req.Caption)
	} else {
		_, err = tui.SendText(ctx, client, store, chat, req.Text)
	}
	return sendResult(display, err)
}

// sendResult turns a send error into a response. A message that was sent
// but not saved to history still counts as sent.
func sendResult(display string, err error) sendResponse {
	switch {
	case err == nil:
		return sendResponse{OK: true, To: display}
	case errors.Is(err, tui.ErrNotSaved):
		return sendResponse{OK: true, To: display + " (" + err.Error() + ")"}
	}
	return sendResponse{Error: err.Error()}
}

// serveControl lets `whatsapp-tui send` use this open app's connection. It
// returns a function that stops listening.
func serveControl(client *whatsmeow.Client, store *history.Store, program *bubbletea.Program) func() {
	// A leftover socket from a crash: remove it unless another app answers.
	if conn, err := net.DialTimeout("unix", controlSocket, 500*time.Millisecond); err == nil {
		conn.Close()
		return func() {} // another app is already serving this account
	}
	_ = os.Remove(controlSocket)
	listener, err := net.Listen("unix", controlSocket)
	if err != nil {
		return func() {} // sending from the shell just won't reach this app
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleControl(conn, client, store, program)
		}
	}()
	return func() {
		listener.Close()
		os.Remove(controlSocket)
	}
}

func handleControl(conn net.Conn, client *whatsmeow.Client, store *history.Store, program *bubbletea.Program) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Minute))
	var req sendRequest
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(sendResponse{Error: "bad request: " + err.Error()})
		return
	}
	// Read contacts fresh: they may have changed in the app since it started.
	saved, err := contacts.LoadContacts(contactsFile)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(sendResponse{Error: "could not load contacts: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	chat, display, err := tui.ResolveChat(ctx, client, store, saved, req.To)
	var record history.Message
	if err == nil {
		if req.File != "" {
			record, err = tui.SendFile(ctx, client, store, chat, req.File, req.Caption)
		} else {
			record, err = tui.SendText(ctx, client, store, chat, req.Text)
		}
	}
	resp := sendResult(display, err)
	if resp.OK {
		program.Send(tui.ExternalSent(record, display))
	}
	_ = json.NewEncoder(conn).Encode(resp)
}
