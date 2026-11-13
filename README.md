# 📱 WhatsApp TUI Messenger (Go + Bubble Tea)

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://golang.org/)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen?logo=github-actions)](https://github.com/yourusername/whatsapp-tui-go/actions)


A terminal-based WhatsApp messenger built in Go using [whatsmeow](https://github.com/tulir/whatsmeow) and the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework. Send messages and files directly from your terminal to WhatsApp contacts with ease.

---

## ✨ Features

- 🔍 Search and filter contacts from a local JSON file
- 💬 WhatsApp-style chat view with sent/received messages
- 🖼️ Inline image previews rendered with truecolor terminal blocks
- 💾 Save incoming images, documents and other media with `Ctrl+S`
- 📎 Send local files with `Ctrl+F` — images are sent as photos, everything else as documents
- 😊 Message reactions — see reactions under bubbles and react to any message with `Ctrl+R`
- 📞 Incoming WhatsApp call alerts with reject (`Ctrl+D`)
- 🔗 Open a direct chat in WhatsApp desktop/web with `Ctrl+O`
- 🗂️ Persistent chat history stored locally in `history.db`
- 💾 Full session persistence in `session.db` (no re-scan after first login)
- 📄 Maintain local contact list in `contacts.json`
- 🎨 Modern WhatsApp-inspired dark UI with avatars, chat bubbles and unread badges
- 🖥️ Smooth terminal UI with keyboard navigation
- 📥 Live incoming messages and unread counts (`Tab` to open chats)
- 🔐 QR-based login (session saved locally)
- 🧠 Written entirely in Go (no browser or Node.js required)

---

## 📸 Screenshots

### 🧭 Home Page
![Home Page](assets/1.png)

### 💬 Message Page
![Message Page](assets/2.png)

### 📤 Final Page
![Final Page](assets/3.png)

---


#### Only Works on Linux System Currently 

Inline previews use truecolor ANSI blocks and look best in a modern terminal (Ghostty, Kitty, WezTerm, Alacritty, etc.).


## 📦 Installation
### 1. Clone the repository
```bash
git clone https://github.com/ashankgupta/whatsapp-tui-go.git
cd whatsapp-tui-go
```
### 2. Install Go modules
```bash
go mod tidy
```
### 3. Build the binary
```bash
go build -o whatsapp-tui
```

`go-sqlite3` needs CGO and a C compiler (for example `gcc`).

### 4. Run the app
```bash
./whatsapp-tui
```

### contacts.json Format
Create a contacts.json file in the root directory with this format:

```json
[
  {
    "name": "Alice",
    "phone": "1234567890"
  },
  {
    "name": "Bob",
    "phone": "9876543210"
  }
]

```
### Important:

#### 1. Phone numbers must be in international format, without + (e.g., 919876543210 for India).

#### 2. No dashes, spaces, or special characters.

## 🔑 QR Code Login

#### 1. On first run, a QR code appears in your terminal.

#### 2. Open WhatsApp → Linked Devices → Link a Device.

#### 3. Scan the code.

#### 4. Your session is saved to `session.db` for reuse.

## 💬 Chat History & Receiving

Messages are saved locally in `history.db`, so conversations survive restarts. Open a contact with `Enter` to see its full history and reply in the same view. Incoming messages arrive live; press `Tab` for a list of recent chats. Reactions are shown under the relevant bubble and persist in `history.db`.

Connection diagnostics are appended to `whatsapp.log` so they do not interfere with the terminal UI.

Incoming images are downloaded automatically and rendered inline using truecolor half-blocks; previews use the available chat width and height, and captions appear beneath the preview. Press `Ctrl+S` to save the latest downloadable media in the open chat. While reaction mode is active, `Ctrl+S` saves the selected message instead. Files are written to `downloads/` (gitignored).

Other incoming media types, such as videos, audio, stickers and documents, appear as labelled bubbles and can be saved on demand. Their original media metadata is stored in `history.db` so they can be re-downloaded later if needed. Images and files you send are cached in the local `media/` folder (gitignored). Images are limited to 16 MB for inline sending; larger images and all other file types are sent as documents.

Incoming WhatsApp calls appear as a banner. You can reject them with `Ctrl+D` or hide the alert with `Esc`. `whatsmeow` does not support starting or answering calls, so `Ctrl+O` opens the contact's chat in WhatsApp Desktop/Web where you can use the call button.

### Keyboard shortcuts

| Screen | Keys |
| --- | --- |
| Contacts | `↑/↓` navigate, `Enter` open chat, `Tab` recent chats, `Ctrl+O` open in WhatsApp, `Esc` clear search, `q` quit |
| Chat | type to compose, `Enter` send, `Ctrl+N` new line, `Ctrl+F` attach a local file (type/paste path, `Enter` send, `Esc` cancel, `Ctrl+U` clear), `Ctrl+S` save latest media (selected media while reacting), `↑/↓` scroll, `Ctrl+R` react (`↑/↓` pick message, `1`–`6` choose emoji, same emoji again removes, `Esc` cancel), `Ctrl+O` open in WhatsApp, `Esc` back, `Ctrl+C` quit |
| Recent chats | `↑/↓` navigate, `Enter` open, `Tab` back, `Ctrl+O` open in WhatsApp, `q` quit |
| Call alert | `Ctrl+D` reject, `Esc` hide |

## ⚠️ Disclaimer

#### This project uses an unofficial WhatsApp Web API, which may stop working if WhatsApp changes its internal protocol. Use responsibly. This is meant for educational or personal use only.

## 🙌 Contributing

Pull requests are welcome! If you have ideas for improvement, feel free to open an issue or PR.


## 📄 License

MIT License
© 2025 Ashank Gupta

