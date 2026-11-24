package tui

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
)

// maxSixelImages bounds the decoded images kept in memory.
const maxSixelImages = 48

// sixelImage is a decoded image scaled to fill whole terminal cells.
type sixelImage struct {
	canvas *image.RGBA
	cols   int
	rows   int
	cellH  int
	crops  map[[2]int]string
}

// sixelMarker tags each placeholder row of an image. It is a valid SGR
// sequence (set and immediately reset the foreground color) so it is
// invisible on its own, and it makes each row's text unique, which forces
// Bubble Tea to repaint image rows whenever the image moves.
func sixelMarker(id, row int) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%d;39m", (id>>8)&0xff, id&0xff, row&0xff)
}

var sixelMarkerRe = regexp.MustCompile(`\x1b\[38;2;(\d+);(\d+);(\d+);39m`)

// sixelPreview reserves cells for an image and returns placeholder rows. The
// pixels are drawn over those cells by placeSixels after the frame is laid out.
func (m Model) sixelPreview(path string, maxCols, maxRows int) (string, error) {
	cw, ch := graphics.cellW, graphics.cellH
	key := fmt.Sprintf("sixel|%s|%d|%d|%dx%d", path, maxCols, maxRows, cw, ch)
	if preview, ok := m.imagePreviews[key]; ok {
		return preview, nil
	}
	if errText, ok := m.imagePreviewErrs[key]; ok {
		return "", fmt.Errorf("%s", errText)
	}

	img, err := loadSixelImage(path, maxCols, maxRows, cw, ch)
	if err != nil {
		m.imagePreviewErrs[key] = err.Error()
		return "", err
	}

	if len(m.sixels) >= maxSixelImages {
		clear(m.sixels)
		for k := range m.imagePreviews {
			if strings.HasPrefix(k, "sixel|") {
				delete(m.imagePreviews, k)
			}
		}
	}
	*m.sixelNextID++
	id := *m.sixelNextID & 0xffff
	m.sixels[id] = img

	pad := strings.Repeat(" ", img.cols)
	rows := make([]string, img.rows)
	for r := range rows {
		rows[r] = sixelMarker(id, r) + pad
	}
	preview := strings.Join(rows, "\n")
	m.imagePreviews[key] = preview
	return preview, nil
}

// loadSixelImage decodes and scales an image to fit maxCols x maxRows cells.
func loadSixelImage(path string, maxCols, maxRows, cw, ch int) (*sixelImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("image has no pixels")
	}

	maxW, maxH := float64(maxCols*cw), float64(maxRows*ch)
	scale := min(maxW/float64(b.Dx()), maxH/float64(b.Dy()), 2)
	w := max(1, int(float64(b.Dx())*scale))
	h := max(1, int(float64(b.Dy())*scale))

	cols := (w + cw - 1) / cw
	rows := (h + ch - 1) / ch
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), src, b, draw.Src, nil)

	return &sixelImage{canvas: canvas, cols: cols, rows: rows, cellH: ch, crops: map[[2]int]string{}}, nil
}

// encode returns the Sixel data for image rows r0..r1 (inclusive). The pixel
// height is rounded down to a multiple of six so the image never spills into
// the cell row below, where text would erase it.
func (img *sixelImage) encode(r0, r1 int) string {
	key := [2]int{r0, r1}
	if s, ok := img.crops[key]; ok {
		return s
	}
	top := r0 * img.cellH
	bottom := min((r1+1)*img.cellH, img.canvas.Bounds().Dy())
	height := (bottom - top) / 6 * 6
	if height <= 0 {
		return ""
	}
	crop := image.NewRGBA(image.Rect(0, 0, img.canvas.Bounds().Dx(), height))
	draw.Draw(crop, crop.Bounds(), img.canvas, image.Pt(0, top), draw.Src)

	var buf bytes.Buffer
	enc := sixel.NewEncoder(&buf)
	enc.Dither = true
	if err := enc.Encode(crop); err != nil {
		return ""
	}
	if len(img.crops) > 8 {
		clear(img.crops)
	}
	img.crops[key] = buf.String()
	return img.crops[key]
}

// placeSixels finds image placeholder rows in a finished frame and draws the
// visible part of each image over them. The Sixel data is attached to the
// line after the image, so Bubble Tea paints it after the placeholder rows;
// the cursor is saved and restored around it so the line renders normally.
func (m Model) placeSixels(view string) string {
	if graphics.mode != graphicsSixel || len(m.sixels) == 0 || !strings.Contains(view, "\x1b[38;2;") {
		return view
	}

	type span struct{ id, l0, l1, r0, r1, col int }
	var spans []*span
	last := map[int]*span{}

	lines := strings.Split(view, "\n")
	for i, line := range lines {
		for _, loc := range sixelMarkerRe.FindAllStringSubmatchIndex(line, -1) {
			hi, _ := strconv.Atoi(line[loc[2]:loc[3]])
			lo, _ := strconv.Atoi(line[loc[4]:loc[5]])
			row, _ := strconv.Atoi(line[loc[6]:loc[7]])
			id := hi<<8 | lo
			if _, ok := m.sixels[id]; !ok {
				continue
			}
			if s := last[id]; s != nil && s.l1 == i-1 && s.r1 == row-1 {
				s.l1, s.r1 = i, row
				continue
			}
			s := &span{id: id, l0: i, l1: i, r0: row, r1: row, col: ansi.StringWidth(line[:loc[0]])}
			spans = append(spans, s)
			last[id] = s
		}
	}

	for _, s := range spans {
		if s.l1+1 >= len(lines) {
			continue
		}
		data := m.sixels[s.id].encode(s.r0, s.r1)
		if data == "" {
			continue
		}
		anchor := "\x1b7" + fmt.Sprintf("\x1b[%d;%dH", s.l0+1, s.col+1) + data + "\x1b8"
		lines[s.l1+1] = anchor + lines[s.l1+1]
	}
	return strings.Join(lines, "\n")
}
