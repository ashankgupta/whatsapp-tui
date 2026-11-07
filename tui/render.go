package tui

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"whatsapp-tui/history"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// truncate shortens plain text to at most max display columns.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}

	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw > max-1 {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	return b.String() + "…"
}

// padBetween places right at the end of a line of the given width.
func padBetween(left, right string, width int) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+rightWidth >= width {
		return left
	}
	return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
}

// initials returns up to two uppercase initials for a name.
func initials(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return "?"
	}
	if len(fields) == 1 {
		runes := []rune(fields[0])
		if len(runes) == 0 {
			return "?"
		}
		return strings.ToUpper(string(runes[0]))
	}
	first := []rune(fields[0])[0]
	second := []rune(fields[1])[0]
	return strings.ToUpper(string(first) + string(second))
}

func avatarColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return avatarColors[int(h.Sum32())%len(avatarColors)]
}

// avatar renders a small colored avatar with the name's initials.
func avatar(name string) string {
	return lipgloss.NewStyle().
		Background(avatarColor(name)).
		Foreground(colorOnAccent).
		Bold(true).
		Width(4).
		Align(lipgloss.Center).
		Render(initials(name))
}

func unreadBadge(count int) string {
	label := fmt.Sprintf("%d", count)
	if count > 99 {
		label = "99+"
	}
	return unreadStyle.Render(label)
}

// listTime formats a timestamp the way chat lists do: time for today,
// "Yesterday", weekday within the last week, otherwise a short date.
func listTime(t time.Time) string {
	now := time.Now()
	day := func(t time.Time) time.Time {
		y, mo, d := t.Date()
		return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
	}
	days := int(day(now).Sub(day(t)).Hours() / 24)
	switch {
	case days <= 0:
		return t.Format("15:04")
	case days == 1:
		return "Yesterday"
	case days < 7:
		return t.Format("Monday")
	default:
		return t.Format("02/01/06")
	}
}

// dayLabel formats the date separator shown between messages.
func dayLabel(t time.Time) string {
	switch l := listTime(t); {
	case strings.Contains(l, ":"):
		return "Today"
	case l == "Yesterday" || !strings.Contains(l, "/"):
		return l
	}
	if t.Year() == time.Now().Year() {
		return t.Format("Mon, 2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// lastConversation returns the most recent stored message for a chat.
func (m Model) lastConversation(chatJID string) (history.Conversation, bool) {
	for _, c := range m.conversations {
		if c.ChatJID == chatJID {
			return c, true
		}
	}
	return history.Conversation{}, false
}

// conversationLabel builds the one-line preview shown in chat lists.
func conversationLabel(c history.Conversation) string {
	label := messageMediaLabel(history.Message{
		Text:      c.Text,
		MediaType: c.MediaType,
		MediaName: c.MediaName,
	})
	return strings.ReplaceAll(label, "\n", " ")
}

// subtitle describes a chat under its name in the chat header.
func (m Model) subtitle(chatJID string) string {
	if strings.HasSuffix(chatJID, "@g.us") {
		return "Group chat"
	}
	if strings.Contains(chatJID, "@") {
		return chatJID
	}
	return "+" + chatJID
}

// renderBar draws a full-width filled bar with left and right content.
func (m Model) renderBar(left, right string) string {
	width := m.terminalWidth
	inner := width - barStyle.GetHorizontalPadding()
	if inner < 1 {
		inner = 1
	}
	content := ansi.Truncate(padBetween(left, right, inner), inner, "…")
	line := barStyle.Width(width).Render(content)
	return withBackground(line, colorBar, width)
}

func (m Model) renderSectionLabel(label, right string) string {
	width := m.terminalWidth - 2
	if width < 1 {
		width = 1
	}
	return " " + padBetween(sectionStyle.Render(label), mutedStyle.Render(right), width)
}

func (m Model) renderSearch() string {
	innerWidth := m.terminalWidth - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	style := searchBarStyle
	query := m.searchQuery
	if query == "" {
		query = mutedStyle.Render("Search contacts")
	} else {
		style = searchBarActiveStyle
		query = normalStyle.Render(truncate(query, innerWidth-6))
	}

	content := accentStyle.Render("⌕ ") + query + accentStyle.Render("▏")
	return style.Width(innerWidth).Render(content)
}

// listRange returns the visible slice bounds for a scrolled list.
func (m Model) listRange(scroll, total int) (int, int) {
	start := scroll
	if start > total {
		start = total
	}
	end := start + m.listRows()
	if end > total {
		end = total
	}
	return start, end
}

func (m Model) renderContactList() string {
	start, end := m.listRange(m.scrollOffset, len(m.filteredContacts))

	var lines []string
	for i := start; i < end; i++ {
		c := m.filteredContacts[i]
		conv, hasConv := m.lastConversation(c.Number)
		preview, when := c.Number, ""
		if hasConv {
			preview = conversationLabel(conv)
			if conv.FromMe {
				preview = "You: " + preview
			}
			when = listTime(conv.Timestamp)
		}
		lines = append(lines, m.renderListRow(c.Name, when, preview, m.unread[c.Number], i == m.selected))
	}
	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("   No matching contacts"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderChatsList() string {
	start, end := m.listRange(m.convScroll, len(m.conversations))

	var lines []string
	for i := start; i < end; i++ {
		c := m.conversations[i]
		preview := conversationLabel(c)
		if c.FromMe {
			preview = "You: " + preview
		}
		lines = append(lines, m.renderListRow(m.chatName(c.ChatJID), listTime(c.Timestamp), preview, m.unread[c.ChatJID], i == m.convSelected))
	}
	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("   No conversations yet"))
	}
	return strings.Join(lines, "\n")
}

// renderListRow draws a two-line chat/contact row: avatar, name and time on
// top, message preview and unread badge below.
func (m Model) renderListRow(name, when, preview string, unread int, selected bool) string {
	const indent = 7 // bar + space + avatar + space
	textWidth := m.terminalWidth - indent - 1
	if textWidth < 10 {
		textWidth = 10
	}

	bar := " "
	if selected {
		bar = accentStyle.Render("▌")
	}

	nameStyle := normalStyle
	timeStyle := mutedStyle
	if selected || unread > 0 {
		nameStyle = titleStyle
	}
	if unread > 0 {
		timeStyle = successTextStyle
	}

	timeStr := timeStyle.Render(when)
	line1 := padBetween(nameStyle.Render(truncate(name, textWidth-lipgloss.Width(timeStr)-1)), timeStr, textWidth)

	badge := ""
	if unread > 0 {
		badge = unreadBadge(unread)
	}
	previewStyle := mutedStyle
	if unread > 0 {
		previewStyle = secondaryStyle
	}
	line2 := padBetween(previewStyle.Render(truncate(preview, textWidth-lipgloss.Width(badge)-1)), badge, textWidth)

	top := bar + " " + avatar(name) + " " + line1
	bottom := bar + strings.Repeat(" ", indent-1) + line2
	if selected {
		top = withBackground(top, colorSelected, m.terminalWidth)
		bottom = withBackground(bottom, colorSelected, m.terminalWidth)
	}
	return top + "\n" + bottom
}

func (m Model) renderChatHeader() string {
	const indent = 6 // avatar + space, inside the bar padding
	textWidth := m.terminalWidth - barStyle.GetHorizontalPadding() - indent
	if textWidth < 10 {
		textWidth = 10
	}

	name := m.chatName(m.activeChat)
	hint := keyStyle.Render("Esc") + mutedStyle.Render(" back")
	line1 := avatar(name) + "  " + padBetween(
		titleStyle.Render(truncate(name, textWidth-lipgloss.Width(hint)-2)),
		hint,
		textWidth,
	)
	line2 := strings.Repeat(" ", indent) + mutedStyle.Render(truncate(m.subtitle(m.activeChat), textWidth))

	width := m.terminalWidth
	return withBackground(barStyle.Width(width).Render(line1+"\n"+line2), colorBar, width)
}

func (m Model) renderChatMessages() string {
	budget := m.chatLineBudget()
	if len(m.chatMessages) == 0 {
		empty := mutedStyle.Render("No messages yet — say hi 👋")
		return lipgloss.Place(m.terminalWidth, budget, lipgloss.Center, lipgloss.Center, empty)
	}

	if m.reacting {
		return padTop(m.renderReactionView(budget), budget)
	}

	start := m.chatScroll
	if start < 0 {
		start = 0
	}
	if start >= len(m.chatMessages) {
		start = len(m.chatMessages) - 1
	}

	atBottom := start >= m.maxChatScroll()
	var blocks []string
	used := 0

	if atBottom {
		for i := len(m.chatMessages) - 1; i >= start; i-- {
			b := m.messageBlock(i, m.savingMedia && i == m.saveTarget)
			h := strings.Count(b, "\n") + 1
			if used+h > budget && len(blocks) > 0 {
				break
			}
			blocks = append(blocks, b)
			used += h
		}
		for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
			blocks[i], blocks[j] = blocks[j], blocks[i]
		}
	} else {
		for i := start; i < len(m.chatMessages); i++ {
			b := m.messageBlock(i, m.savingMedia && i == m.saveTarget)
			h := strings.Count(b, "\n") + 1
			if used+h > budget && len(blocks) > 0 {
				break
			}
			blocks = append(blocks, b)
			used += h
			if used >= budget {
				break
			}
		}
	}

	lines := strings.Split(strings.Join(blocks, "\n"), "\n")
	if len(lines) > budget {
		if atBottom {
			lines = lines[len(lines)-budget:]
		} else {
			lines = lines[:budget]
		}
	}
	return padTop(strings.Join(lines, "\n"), budget)
}

// padTop pushes content to the bottom of an area of the given height, so a
// short conversation sits just above the input like in mobile chat apps.
func padTop(content string, height int) string {
	if missing := height - (strings.Count(content, "\n") + 1); missing > 0 {
		return strings.Repeat("\n", missing) + content
	}
	return content
}

// renderReactionView shows the selected message with surrounding context.
func (m Model) renderReactionView(budget int) string {
	target := m.reactionTarget
	if target < 0 {
		target = 0
	}
	if target >= len(m.chatMessages) {
		target = len(m.chatMessages) - 1
	}

	targetBlock := m.messageBlock(target, true)
	targetLines := strings.Split(targetBlock, "\n")
	if len(targetLines) > budget {
		return strings.Join(targetLines[:budget], "\n")
	}

	used := len(targetLines)
	blocks := []string{targetBlock}

	aboveLimit := budget / 2
	aboveUsed := 0
	var above []string
	for i := target - 1; i >= 0; i-- {
		b := m.messageBlock(i, false)
		h := strings.Count(b, "\n") + 1
		if aboveUsed+h > aboveLimit {
			break
		}
		above = append(above, b)
		aboveUsed += h
	}
	for i, j := 0, len(above)-1; i < j; i, j = i+1, j-1 {
		above[i], above[j] = above[j], above[i]
	}
	blocks = append(above, blocks...)
	used += aboveUsed

	for i := target + 1; i < len(m.chatMessages); i++ {
		b := m.messageBlock(i, false)
		h := strings.Count(b, "\n") + 1
		if used+h > budget {
			break
		}
		blocks = append(blocks, b)
		used += h
	}

	return strings.Join(blocks, "\n")
}

// messageBlock renders message i, preceded by a date separator when it is
// the first message of a new day.
func (m Model) messageBlock(i int, selected bool) string {
	msg := m.chatMessages[i]
	bubble := m.renderMessageBubble(msg, selected)

	if i > 0 {
		py, pm, pd := m.chatMessages[i-1].Timestamp.Date()
		y, mo, d := msg.Timestamp.Date()
		if py == y && pm == mo && pd == d {
			return bubble
		}
	}
	chip := dateChipStyle.Render(dayLabel(msg.Timestamp))
	return lipgloss.PlaceHorizontal(m.terminalWidth, lipgloss.Center, chip) + "\n" + bubble
}

func (m Model) bubbleWidths() (int, int) {
	maxTotal := m.terminalWidth * 2 / 3
	if maxTotal > m.terminalWidth-6 {
		maxTotal = m.terminalWidth - 6
	}
	if maxTotal < 18 {
		maxTotal = m.terminalWidth - 4
	}
	if maxTotal < 12 {
		maxTotal = 12
	}

	maxStyleWidth := maxTotal - 2 // account for the rounded border
	if maxStyleWidth < 8 {
		maxStyleWidth = 8
	}
	return maxTotal, maxStyleWidth
}

func (m Model) renderMessageBubble(msg history.Message, selected bool) string {
	if msg.MediaType != "" {
		return m.renderMediaBubble(msg, selected)
	}
	return m.renderBubble(msg, m.withSender(msg, msg.Text), 0, selected)
}

// withSender prefixes group messages from others with the sender's name,
// colored to match their avatar.
func (m Model) withSender(msg history.Message, content string) string {
	if !strings.Contains(msg.ChatJID, "@g.us") || msg.FromMe {
		return content
	}
	sender := singleLine(m.senderName(msg))
	if sender == "" {
		return content
	}
	label := lipgloss.NewStyle().Foreground(avatarColor(sender)).Bold(true).Render(sender)
	return label + "\n" + content
}

func (m Model) renderMediaBubble(msg history.Message, selected bool) string {
	_, maxStyleWidth := m.bubbleWidths()
	content := messageMediaLabel(msg)

	if msg.MediaType == "image" {
		if m.mediaPending[msg.WaID] {
			content += "\n" + mutedStyle.Render("⏳ Downloading image...")
		} else if errText := m.mediaErrors[msg.WaID]; errText != "" {
			content += "\n" + errorTextStyle.Render("❌ "+errText)
		}
	}
	content = m.withSender(msg, content)

	var imageBlock string
	imageWidth := 0
	if msg.MediaType == "image" && msg.MediaPath != "" && !m.mediaPending[msg.WaID] {
		maxRows := maxImageRows
		if budget := m.chatLineBudget(); budget > 0 {
			reserve := mediaBubbleReserve(content, maxStyleWidth-2)
			if maxRows > budget-reserve {
				maxRows = budget - reserve
			}
		}
		if maxRows < 3 {
			maxRows = 3
		}
		maxCols := m.terminalWidth - 4
		if maxCols > maxImageCols {
			maxCols = maxImageCols
		}
		if maxCols < 8 {
			maxCols = 8
		}
		if preview, err := m.imagePreview(msg.MediaPath, maxCols, maxRows); err == nil {
			imageBlock = preview
			if first, _, _ := strings.Cut(preview, "\n"); first != "" {
				imageWidth = lipgloss.Width(first)
			}
		} else {
			content += "\n" + mutedStyle.Render("Preview unavailable")
		}
	}

	bubble := m.renderBubble(msg, content, imageWidth, selected)
	if imageBlock == "" {
		return bubble
	}

	align := lipgloss.Left
	if msg.FromMe {
		align = lipgloss.Right
	}
	lines := strings.Split(imageBlock, "\n")
	for i := range lines {
		lines[i] = lipgloss.PlaceHorizontal(m.terminalWidth, align, lines[i])
	}
	return strings.Join(lines, "\n") + "\n" + bubble
}

func mediaBubbleReserve(content string, width int) int {
	if width < 1 {
		width = 1
	}
	lines := 0
	for _, line := range strings.Split(content, "\n") {
		w := lipgloss.Width(line)
		if w <= 0 {
			lines++
			continue
		}
		lines += (w + width - 1) / width
	}
	return lines + 3
}

func (m Model) renderBubble(msg history.Message, content string, forcedWidth int, selected bool) string {
	_, maxStyleWidth := m.bubbleWidths()

	reactionText := reactionSummary(msg.Reactions)
	contentWidth := lipgloss.Width(content)
	if w := lipgloss.Width(reactionText); w > contentWidth {
		contentWidth = w
	}

	styleWidth := contentWidth + 2 // + horizontal padding
	if forcedWidth > styleWidth {
		styleWidth = forcedWidth
	}
	if styleWidth > maxStyleWidth {
		styleWidth = maxStyleWidth
	}
	if styleWidth < 10 {
		styleWidth = 10
	}
	innerWidth := styleWidth - 2
	if innerWidth < 6 {
		innerWidth = 6
	}

	bubbleStyle := receivedBubbleStyle
	fill := colorSurface
	timeStyle := bubbleTimeRecvStyle
	reactionStyle := bubbleReactionRecvStyle
	align := lipgloss.Left
	if msg.FromMe {
		bubbleStyle = sentBubbleStyle
		fill = colorSent
		timeStyle = bubbleTimeSentStyle
		reactionStyle = bubbleReactionSentStyle
		align = lipgloss.Right
	}
	if selected {
		bubbleStyle = bubbleStyle.BorderForeground(colorAccentBright)
	}

	body := content
	if reactionText != "" {
		body += "\n" + reactionStyle.Width(innerWidth).Render(reactionText)
	}
	timeLine := timeStyle.Width(innerWidth).Align(lipgloss.Right).Render(msg.Timestamp.Format("15:04"))
	bubble := bubbleStyle.Width(styleWidth).Render(withBackground(body+"\n"+timeLine, fill, 0))

	// Keep a one-column gutter from the screen edge.
	if align == lipgloss.Right {
		return lipgloss.PlaceHorizontal(m.terminalWidth-1, align, bubble)
	}
	return lipgloss.PlaceHorizontal(m.terminalWidth, align, lipgloss.NewStyle().PaddingLeft(1).Render(bubble))
}

// reactionSummary renders reactions as emoji counts, e.g. "👍 2  ❤️".
func reactionSummary(reactions map[string]string) string {
	if len(reactions) == 0 {
		return ""
	}

	counts := make(map[string]int)
	for _, emoji := range reactions {
		if emoji != "" {
			counts[emoji]++
		}
	}
	if len(counts) == 0 {
		return ""
	}

	type total struct {
		emoji string
		count int
	}
	totals := make([]total, 0, len(counts))
	for emoji, count := range counts {
		totals = append(totals, total{emoji: emoji, count: count})
	}
	sort.Slice(totals, func(i, j int) bool {
		if totals[i].count != totals[j].count {
			return totals[i].count > totals[j].count
		}
		return totals[i].emoji < totals[j].emoji
	})

	parts := make([]string, len(totals))
	for i, t := range totals {
		if t.count > 1 {
			parts[i] = fmt.Sprintf("%s %d", t.emoji, t.count)
		} else {
			parts[i] = t.emoji
		}
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderInput() string {
	innerWidth := m.terminalWidth - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	cursor := accentStyle.Render("▏")

	if m.attaching {
		text := m.attachInput
		if text == "" {
			text = mutedStyle.Render("Type or paste a file path")
		} else {
			text = normalStyle.Render(truncate(text, max(4, innerWidth-6)))
		}
		return inputBarStyle.Width(innerWidth).Render(accentStyle.Render("📎 ") + text + cursor)
	}

	lines := m.messageLines
	if len(lines) == 0 {
		lines = []string{""}
	}

	rendered := make([]string, 0, len(lines))
	for i, line := range lines {
		if len(lines) == 1 && line == "" {
			rendered = append(rendered, cursor+mutedStyle.Render("Type a message"))
			continue
		}
		text := normalStyle.Render(line)
		if i == m.currentLine {
			text += cursor
		}
		rendered = append(rendered, text)
	}
	return inputBarStyle.Width(innerWidth).Render(strings.Join(rendered, "\n"))
}

func (m Model) renderCallBanner() string {
	if m.activeCall == nil {
		return ""
	}
	caller := m.chatName(m.activeCall.Caller)
	label := "📞 Incoming call"
	if m.activeCall.IsVideo {
		label = "📹 Incoming video call"
	}
	if m.activeCall.IsGroup {
		label += " (group)"
	}
	text := label + " from " + caller
	hint := "^D reject · Esc hide"

	width := m.terminalWidth - 2
	if width < 10 {
		width = 10
	}
	inner := width - 2
	if inner < 6 {
		inner = 6
	}
	if lipgloss.Width(text)+lipgloss.Width(hint)+2 > inner {
		avail := inner - lipgloss.Width(hint) - 2
		if avail >= 12 {
			text = truncate(text, avail)
		} else {
			hint = ""
		}
	}
	return callBannerStyle.Width(width).Render(padBetween(text, hint, inner))
}

func (m Model) renderStatus() string {
	if m.isSending || m.isUploading || m.isSavingMedia {
		return accentStyle.Render("● " + m.statusMessage)
	}
	if m.statusMessage == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(m.statusMessage, "❌"):
		return errorTextStyle.Render("✗ " + strings.TrimSpace(strings.TrimPrefix(m.statusMessage, "❌")))
	case strings.HasPrefix(m.statusMessage, "✅"):
		return successTextStyle.Render("✓ " + strings.TrimSpace(strings.TrimPrefix(m.statusMessage, "✅")))
	case strings.HasPrefix(m.statusMessage, "⚠️"):
		return warningTextStyle.Render("⚠ " + strings.TrimSpace(strings.TrimPrefix(m.statusMessage, "⚠️")))
	default:
		return secondaryStyle.Render(m.statusMessage)
	}
}

// renderHints turns "Key  Label  •  Key  Label" into styled key hints,
// dropping trailing hints that don't fit in width.
func renderHints(hints string, width int) string {
	sep := mutedStyle.Render("  ·  ")
	out := ""
	for _, hint := range strings.Split(hints, "  •  ") {
		key, label, _ := strings.Cut(strings.TrimSpace(hint), "  ")
		part := keyStyle.Render(key)
		if label != "" {
			part += " " + mutedStyle.Render(strings.TrimSpace(label))
		}
		next := part
		if out != "" {
			next = out + sep + part
		}
		if lipgloss.Width(next) > width {
			break
		}
		out = next
	}
	return out
}

func (m Model) renderFooter(hints string) string {
	inner := m.terminalWidth - barStyle.GetHorizontalPadding()
	status := m.renderStatus()
	if status == "" {
		return m.renderBar(renderHints(hints, inner), "")
	}
	remaining := inner - lipgloss.Width(status) - 3
	if remaining > 12 {
		return m.renderBar(status, renderHints(hints, remaining))
	}
	return m.renderBar(status, "")
}
