package tui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"whatsapp-tui/history"

	bubbletea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const (
	mediaDir            = "media"
	downloadsDir        = "downloads"
	maxInlineImageBytes = 16 << 20
	maxImageCols        = 160
	maxImageRows        = 60
)

// encodeMediaRaw serializes the original protobuf so media can be downloaded later.
func encodeMediaRaw(raw *waE2E.Message) []byte {
	if raw == nil {
		return nil
	}
	data, err := proto.Marshal(raw)
	if err != nil {
		return nil
	}
	return data
}

// rawMediaMIME returns the MIME type declared on a WhatsApp media message.
func rawMediaMIME(raw *waE2E.Message) string {
	if raw == nil {
		return ""
	}
	switch {
	case raw.GetDocumentMessage() != nil:
		return raw.GetDocumentMessage().GetMimetype()
	case raw.GetImageMessage() != nil:
		return raw.GetImageMessage().GetMimetype()
	case raw.GetVideoMessage() != nil:
		return raw.GetVideoMessage().GetMimetype()
	case raw.GetAudioMessage() != nil:
		return raw.GetAudioMessage().GetMimetype()
	case raw.GetStickerMessage() != nil:
		return raw.GetStickerMessage().GetMimetype()
	}
	return ""
}

// fileExists reports whether path points at a regular file.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// resolveLocalPath expands ~ and file:// paths and verifies the target.
func resolveLocalPath(input string) (string, error) {
	path := strings.TrimSpace(input)
	path = strings.Trim(path, `"'`)
	if path == "" {
		return "", fmt.Errorf("enter a file path")
	}

	if strings.HasPrefix(path, "file://") {
		u, err := url.Parse(path)
		if err != nil || u.Path == "" {
			return "", fmt.Errorf("invalid file URL")
		}
		path = u.Path
	}

	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	return abs, nil
}

// messageMediaLabel describes a media message in chat bubbles and previews.
func messageMediaLabel(msg history.Message) string {
	switch msg.MediaType {
	case "image":
		caption := strings.TrimSpace(msg.Text)
		if caption == "" || caption == "[image]" {
			return "📷 Photo"
		}
		if strings.HasPrefix(caption, "📷 ") {
			return caption
		}
		return "📷 " + caption
	case "document":
		name := strings.TrimSpace(msg.MediaName)
		if name == "" {
			name = strings.TrimSpace(strings.TrimPrefix(msg.Text, "📄 "))
		}
		displayName := singleLine(name)
		label := "📄 Document"
		if displayName != "" && displayName != "[document]" {
			label = "📄 " + displayName
		}
		if msg.Text != "" && msg.Text != "[document]" && msg.Text != label && name != msg.Text {
			label += "\n" + msg.Text
		}
		return label
	case "video":
		if msg.Text != "" && msg.Text != "[video]" && msg.Text != "🎥 Video" {
			return "🎥 " + msg.Text
		}
		return "🎥 Video"
	case "audio":
		if msg.Text == "" || msg.Text == "[audio]" {
			return "🎵 Audio"
		}
		return msg.Text
	case "sticker":
		return "🧩 Sticker"
	default:
		return msg.Text
	}
}

// singleLine collapses whitespace so filenames cannot break the bubble layout.
func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// imagePreview renders a cached ANSI preview of an image for the terminal.
func (m Model) imagePreview(path string, maxCols, maxRows int) (string, error) {
	if maxCols < 2 || maxRows < 1 {
		return "", fmt.Errorf("preview is too small")
	}

	key := fmt.Sprintf("%s|%d|%d", path, maxCols, maxRows)
	if preview, ok := m.imagePreviews[key]; ok {
		return preview, nil
	}
	if errText, ok := m.imagePreviewErrs[key]; ok {
		return "", fmt.Errorf("%s", errText)
	}

	preview, err := renderImageFile(path, maxCols, maxRows)
	if err != nil {
		if m.imagePreviewErrs != nil {
			m.imagePreviewErrs[key] = err.Error()
		}
		return "", err
	}
	if m.imagePreviews != nil {
		m.imagePreviews[key] = preview
	}
	return preview, nil
}

// renderImageFile decodes an image and converts it to truecolor half blocks.
func renderImageFile(path string, maxCols, maxRows int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}

	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return "", fmt.Errorf("image has no pixels")
	}

	scale := 1.0
	if srcW > maxCols {
		scale = float64(maxCols) / float64(srcW)
	}
	maxPixelRows := maxRows * 2
	if float64(srcH)*scale > float64(maxPixelRows) {
		scale = min(scale, float64(maxPixelRows)/float64(srcH))
	}
	// Allow scaling up slightly for very small images to look sharper
	if scale < 1.0 {
		scale = scale * 1.0
	}

	dstW := max(1, int(float64(srcW)*scale))
	dstH := max(2, int(float64(srcH)*scale))
	if dstH%2 != 0 {
		dstH++
	}
	if dstH < 2 {
		dstH = 2
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	scaleImage(src, dst)
	return imageToHalfBlocks(dst), nil
}

// scaleImage resizes src into dst using bilinear-like sampling for better quality.
func scaleImage(src image.Image, dst *image.RGBA) {
	sb := src.Bounds()
	db := dst.Bounds()

	if db.Dx() == 0 || db.Dy() == 0 {
		return
	}

	sw := sb.Dx()
	sh := sb.Dy()
	dw := db.Dx()
	dh := db.Dy()

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			fx := float64(x) * float64(sw) / float64(dw)
			fy := float64(y) * float64(sh) / float64(dh)
			x0 := int(fx)
			y0 := int(fy)
			x1 := x0 + 1
			y1 := y0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			if y1 >= sh {
				y1 = sh - 1
			}
			if x0 >= sw {
				x0 = sw - 1
			}
			if y0 >= sh {
				y0 = sh - 1
			}
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}

			r0, g0, b0 := blendOnBackground(src.At(sb.Min.X+x0, sb.Min.Y+y0))
			r1, g1, b1 := blendOnBackground(src.At(sb.Min.X+x1, sb.Min.Y+y0))
			r2, g2, b2 := blendOnBackground(src.At(sb.Min.X+x0, sb.Min.Y+y1))
			r3, g3, b3 := blendOnBackground(src.At(sb.Min.X+x1, sb.Min.Y+y1))

			wx := fx - float64(x0)
			wy := fy - float64(y0)
			r := uint8((float64(r0)*(1-wx)*(1-wy) + float64(r1)*wx*(1-wy) + float64(r2)*(1-wx)*wy + float64(r3)*wx*wy))
			g := uint8((float64(g0)*(1-wx)*(1-wy) + float64(g1)*wx*(1-wy) + float64(g2)*(1-wx)*wy + float64(g3)*wx*wy))
			b := uint8((float64(b0)*(1-wx)*(1-wy) + float64(b1)*wx*(1-wy) + float64(b2)*(1-wx)*wy + float64(b3)*wx*wy))
			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
}

// sampleRegion averages a 4x4 grid of pixels and blends them onto the app
// background so transparent images do not show terminal artifacts.
func sampleRegion(src image.Image, x0, x1, y0, y1 int) (uint8, uint8, uint8) {
	const samples = 4
	var sumR, sumG, sumB uint64
	count := uint64(0)

	for j := 0; j < samples; j++ {
		py := y0 + (y1-y0)*(2*j+1)/(2*samples)
		if py >= y1 {
			py = y1 - 1
		}
		if py < y0 {
			py = y0
		}
		for i := 0; i < samples; i++ {
			px := x0 + (x1-x0)*(2*i+1)/(2*samples)
			if px >= x1 {
				px = x1 - 1
			}
			if px < x0 {
				px = x0
			}

			r, g, b := blendOnBackground(src.At(px, py))
			sumR += uint64(r)
			sumG += uint64(g)
			sumB += uint64(b)
			count++
		}
	}

	if count == 0 {
		return 0x0B, 0x14, 0x1A
	}
	return uint8(sumR / count), uint8(sumG / count), uint8(sumB / count)
}

// blendOnBackground composites a color onto the app's dark background.
func blendOnBackground(c color.Color) (uint8, uint8, uint8) {
	const bgR, bgG, bgB = 0x0B, 0x14, 0x1A
	r, g, b, a := c.RGBA()
	if a == 0 {
		return bgR, bgG, bgB
	}

	a8 := a >> 8
	br := uint32(r>>8) + uint32(bgR)*(255-a8)/255
	bg := uint32(g>>8) + uint32(bgG)*(255-a8)/255
	bb := uint32(b>>8) + uint32(bgB)*(255-a8)/255
	if br > 255 {
		br = 255
	}
	if bg > 255 {
		bg = 255
	}
	if bb > 255 {
		bb = 255
	}
	return uint8(br), uint8(bg), uint8(bb)
}

// imageToHalfBlocks renders two vertical pixels per terminal cell.
func imageToHalfBlocks(img *image.RGBA) string {
	var b strings.Builder
	bounds := img.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			top := img.RGBAAt(x, y)
			bottom := img.RGBAAt(x, y+1)
			fmt.Fprintf(
				&b,
				"\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bottom.R, bottom.G, bottom.B,
			)
		}
		b.WriteString("\x1b[0m")
		if y+2 < bounds.Max.Y {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// makeJPEGThumbnail creates the small preview WhatsApp expects on images.
func makeJPEGThumbnail(src image.Image, maxSize int) []byte {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}

	scale := min(1.0, min(float64(maxSize)/float64(w), float64(maxSize)/float64(h)))
	tw := max(1, int(float64(w)*scale))
	th := max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	scaleImage(src, dst)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 70}); err != nil {
		return nil
	}
	return buf.Bytes()
}

// attachment describes a local file selected for upload.
type attachment struct {
	Path  string
	Name  string
	MIME  string
	Size  int64
	Kind  string
	Data  []byte
	Image image.Image
}

// inspectAttachment determines whether a local file should be sent as an
// inline image or as a document.
func inspectAttachment(path string) (*attachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory")
	}

	att := &attachment{
		Path: path,
		Name: filepath.Base(path),
		Size: info.Size(),
		Kind: "document",
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 512)
	n, readErr := f.Read(header)
	f.Close()
	if readErr != nil && readErr != io.EOF {
		return nil, readErr
	}
	att.MIME = detectMIME(path, header[:n])
	if att.MIME == "" {
		att.MIME = "application/octet-stream"
	}

	if !isInlineImageMIME(att.MIME) || info.Size() > maxInlineImageBytes {
		return att, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return att, nil
	}
	switch format {
	case "jpeg":
		att.MIME = "image/jpeg"
	case "png":
		att.MIME = "image/png"
	default:
		return att, nil
	}

	att.Kind = "image"
	att.Data = data
	att.Image = img
	return att, nil
}

func isInlineImageMIME(mimeType string) bool {
	return mimeType == "image/jpeg" || mimeType == "image/png"
}

func detectMIME(path string, header []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		if base, _, err := mime.ParseMediaType(mimeType); err == nil {
			mimeType = base
		}
		if mimeType != "" {
			return mimeType
		}
	}
	if len(header) > 0 {
		detected := http.DetectContentType(header)
		if base, _, err := mime.ParseMediaType(detected); err == nil {
			detected = base
		}
		return detected
	}
	return "application/octet-stream"
}

// cacheMediaBytes stores decrypted incoming media in the local media folder.
func cacheMediaBytes(id, name, mimeType string, data []byte) (string, error) {
	path, err := mediaPath(id, name, mimeType)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// cacheMediaFile copies an outgoing attachment into the local media folder.
func cacheMediaFile(id, name, mimeType, src string) (string, error) {
	path, err := mediaPath(id, name, mimeType)
	if err != nil {
		return "", err
	}

	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func mediaPath(id, name, mimeType string) (string, error) {
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return "", err
	}
	filename := safeFilePart(id)
	if filename == "" {
		filename = fmt.Sprintf("media-%d", time.Now().UnixNano())
	}
	return filepath.Join(mediaDir, filename+extensionFor(name, mimeType)), nil
}

func safeFilePart(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 72 {
			break
		}
	}
	return b.String()
}

func extensionFor(name, mimeType string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if isSafeExtension(ext) {
		return ext
	}
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}

func isSafeExtension(ext string) bool {
	if len(ext) < 2 || len(ext) > 12 || ext[0] != '.' {
		return false
	}
	for _, r := range ext[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// mediaResult is delivered after an incoming image download finishes.
type mediaResult struct {
	waID      string
	chatJID   string
	mediaType string
	mediaName string
	path      string
	note      string
	err       error
}

// downloadImageCmd fetches, decrypts and caches an incoming image.
func (m Model) downloadImageCmd(record history.Message, raw *waE2E.Message) bubbletea.Cmd {
	return func() bubbletea.Msg {
		result := mediaResult{
			waID:      record.WaID,
			chatJID:   record.ChatJID,
			mediaType: "image",
			mediaName: record.MediaName,
		}
		if record.WaID == "" {
			result.err = fmt.Errorf("message has no WhatsApp ID")
			return result
		}
		if m.client == nil || raw == nil {
			result.err = fmt.Errorf("not connected")
			return result
		}

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		data, err := m.client.DownloadAny(ctx, raw)
		if err != nil {
			if image := raw.GetImageMessage(); image != nil {
				if thumb := image.GetJPEGThumbnail(); len(thumb) > 0 {
					if thumbPath, cacheErr := cacheMediaBytes(record.WaID+"-thumb", "", "image/jpeg", thumb); cacheErr == nil {
						result.path = thumbPath
						result.note = "full image unavailable; showing thumbnail"
						return result
					}
				}
			}
			result.err = err
			return result
		}

		mimeType := http.DetectContentType(data)
		path, err := cacheMediaBytes(record.WaID, "", mimeType, data)
		if err != nil {
			result.err = err
			return result
		}
		result.path = path
		return result
	}
}

// sanitizeDownloadName makes a user-supplied media name safe for downloads.
func sanitizeDownloadName(name string) string {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	name = filepath.Base(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			b.WriteRune('_')
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case r < 32 || r == 127:
			continue
		default:
			b.WriteRune(r)
		}
	}
	cleaned := strings.Join(strings.Fields(b.String()), " ")
	cleaned = strings.Trim(cleaned, ". ")
	if cleaned == "" {
		return ""
	}
	const maxNameBytes = 180
	if len(cleaned) <= maxNameBytes {
		return cleaned
	}
	ext := filepath.Ext(cleaned)
	if !isSafeExtension(ext) {
		ext = ""
	}
	stem := strings.TrimSuffix(cleaned, ext)
	maxStem := maxNameBytes - len(ext)
	if maxStem < 1 {
		maxStem = 1
	}
	for len(stem) > maxStem {
		_, size := utf8.DecodeLastRuneInString(stem)
		if size == 0 {
			break
		}
		stem = stem[:len(stem)-size]
	}
	return stem + ext
}

// downloadFilename chooses a useful, extension-bearing filename for saved media.
func downloadFilename(rec history.Message, mimeType string) string {
	name := sanitizeDownloadName(rec.MediaName)
	if name == "" {
		id := safeFilePart(rec.WaID)
		if id == "" {
			id = fmt.Sprintf("%d", time.Now().Unix())
		}
		kind := rec.MediaType
		if kind == "" {
			kind = "media"
		}
		name = kind + "-" + id
	}
	if ext := filepath.Ext(name); !isSafeExtension(ext) {
		hint := name
		if rec.MediaPath != "" {
			hint = rec.MediaPath
		}
		name += extensionFor(hint, mimeType)
	}
	return name
}

// uniqueDownloadPath returns a path in dir that does not already exist.
func uniqueDownloadPath(dir, name string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		path := filepath.Join(dir, candidate)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return path, nil
			}
			return "", err
		}
	}
	return "", fmt.Errorf("could not find a free filename")
}

// mediaSaveResult is delivered after a media file is saved to downloads/.
type mediaSaveResult struct {
	err        error
	path       string
	cachedPath string
	chatJID    string
	waID       string
	mediaType  string
	mediaName  string
}

// saveMediaCmd reads cached media or re-downloads it from WhatsApp and saves a copy.
func (m Model) saveMediaCmd(rec history.Message) bubbletea.Cmd {
	return func() bubbletea.Msg {
		result := mediaSaveResult{
			chatJID:   rec.ChatJID,
			waID:      rec.WaID,
			mediaType: rec.MediaType,
			mediaName: rec.MediaName,
		}

		var (
			data     []byte
			srcPath  string
			mimeType string
		)
		if fileExists(rec.MediaPath) {
			srcPath = rec.MediaPath
			mimeType = detectMIME(srcPath, nil)
		} else {
			if len(rec.MediaRaw) == 0 || m.client == nil {
				result.err = fmt.Errorf("media is no longer available")
				return result
			}
			var raw waE2E.Message
			if err := proto.Unmarshal(rec.MediaRaw, &raw); err != nil {
				result.err = fmt.Errorf("invalid media data: %w", err)
				return result
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var err error
			data, err = m.client.DownloadAny(ctx, &raw)
			if err != nil {
				result.err = err
				return result
			}
			if len(data) == 0 {
				result.err = fmt.Errorf("media file is empty")
				return result
			}
			mimeType = rawMediaMIME(&raw)
			if mimeType == "" {
				mimeType = detectMIME("", data[:min(len(data), 512)])
			}
			if cached, cacheErr := cacheMediaBytes(rec.WaID, rec.MediaName, mimeType, data); cacheErr == nil {
				result.cachedPath = cached
			}
		}

		name := downloadFilename(rec, mimeType)
		if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
			result.err = err
			return result
		}
		path, err := uniqueDownloadPath(downloadsDir, name)
		if err != nil {
			result.err = err
			return result
		}
		if srcPath != "" {
			if err := copyFile(srcPath, path); err != nil {
				result.err = err
				return result
			}
		} else if err := os.WriteFile(path, data, 0o644); err != nil {
			result.err = err
			return result
		}
		result.path = path
		result.mediaName = filepath.Base(path)
		return result
	}
}

// copyFile copies src to dst and removes a partial destination on failure.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// fileSendResult is delivered after an attachment upload finishes.
type fileSendResult struct {
	err       error
	warn      string
	chatJID   string
	text      string
	msgID     string
	time      time.Time
	mediaType string
	mediaPath string
	mediaName string
}

// sendFileCmd uploads a local file as an image or document.
func (m Model) sendFileCmd(path string) bubbletea.Cmd {
	return func() bubbletea.Msg {
		result := fileSendResult{chatJID: m.activeChat}

		if m.client == nil || m.activeChat == "" {
			result.err = fmt.Errorf("no chat selected")
			return result
		}

		att, err := inspectAttachment(path)
		if err != nil {
			result.err = err
			return result
		}

		recipient, err := m.recipientJID()
		if err != nil {
			result.err = err
			return result
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		var (
			outgoing  *waE2E.Message
			mediaType = att.Kind
		)

		switch att.Kind {
		case "image":
			resp, err := m.client.Upload(ctx, att.Data, whatsmeow.MediaImage)
			if err != nil {
				result.err = err
				return result
			}
			bounds := att.Image.Bounds()
			imageMsg := &waE2E.ImageMessage{
				URL:           proto.String(resp.URL),
				DirectPath:    proto.String(resp.DirectPath),
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    proto.Uint64(resp.FileLength),
				Mimetype:      proto.String(att.MIME),
				Width:         proto.Uint32(uint32(bounds.Dx())),
				Height:        proto.Uint32(uint32(bounds.Dy())),
				JPEGThumbnail: makeJPEGThumbnail(att.Image, 32),
			}
			outgoing = &waE2E.Message{ImageMessage: imageMsg}
		default:
			mediaType = "document"
			f, err := os.Open(att.Path)
			if err != nil {
				result.err = err
				return result
			}
			resp, err := m.client.UploadReader(ctx, f, nil, whatsmeow.MediaDocument)
			f.Close()
			if err != nil {
				result.err = err
				return result
			}
			documentMsg := &waE2E.DocumentMessage{
				URL:           proto.String(resp.URL),
				DirectPath:    proto.String(resp.DirectPath),
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    proto.Uint64(resp.FileLength),
				Mimetype:      proto.String(att.MIME),
				FileName:      proto.String(att.Name),
				Title:         proto.String(att.Name),
			}
			outgoing = &waE2E.Message{DocumentMessage: documentMsg}
		}

		resp, err := m.client.SendMessage(ctx, recipient, outgoing)
		if err != nil {
			result.err = err
			return result
		}

		id := string(resp.ID)
		if id == "" {
			id = fmt.Sprintf("out-%d", time.Now().UnixNano())
		}

		var cachePath string
		if mediaType == "image" {
			cachePath, err = cacheMediaBytes(id, att.Name, att.MIME, att.Data)
		} else {
			cachePath, err = cacheMediaFile(id, att.Name, att.MIME, att.Path)
		}
		if err != nil {
			result.warn = "sent, but could not cache file: " + err.Error()
			cachePath = att.Path
		}

		text := "📷 Photo"
		if mediaType == "document" {
			text = "📄 " + att.Name
		}

		result.text = text
		result.msgID = id
		result.time = resp.Timestamp
		result.mediaType = mediaType
		result.mediaPath = cachePath
		result.mediaName = att.Name
		return result
	}
}
