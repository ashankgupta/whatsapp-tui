package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Dark theme based on the WhatsApp desktop palette: deep slate surfaces,
// teal accent, and soft off-white text for long reading sessions.
var (
	colorBar           = lipgloss.Color("#1f2c34") // header / footer bars
	colorSurface       = lipgloss.Color("#202c33") // received bubbles, inputs
	colorSelected      = lipgloss.Color("#2a3942") // highlighted list rows
	colorBorder        = lipgloss.Color("#3b4a54")
	colorText          = lipgloss.Color("#e9edef")
	colorTextSecondary = lipgloss.Color("#aebac1")
	colorTextTertiary  = lipgloss.Color("#8696a0")
	colorAccent        = lipgloss.Color("#00a884")
	colorAccentBright  = lipgloss.Color("#25d366")
	colorOnAccent      = lipgloss.Color("#0b141a")
	colorSent          = lipgloss.Color("#005c4b") // sent bubbles
	colorSentMuted     = lipgloss.Color("#8bc9b9") // timestamps on sent bubbles
	colorError         = lipgloss.Color("#f15c6d")
	colorWarning       = lipgloss.Color("#ffbc38")
	colorInfo          = lipgloss.Color("#53bdeb")
)

// Text styles
var (
	brandStyle       = lipgloss.NewStyle().Foreground(colorAccentBright).Bold(true)
	titleStyle       = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	normalStyle      = lipgloss.NewStyle().Foreground(colorText)
	secondaryStyle   = lipgloss.NewStyle().Foreground(colorTextSecondary)
	mutedStyle       = lipgloss.NewStyle().Foreground(colorTextTertiary)
	accentStyle      = lipgloss.NewStyle().Foreground(colorAccent)
	accentBoldStyle  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	errorTextStyle   = lipgloss.NewStyle().Foreground(colorError)
	successTextStyle = lipgloss.NewStyle().Foreground(colorAccentBright)
	warningTextStyle = lipgloss.NewStyle().Foreground(colorWarning)
	sectionStyle     = lipgloss.NewStyle().Foreground(colorTextTertiary).Bold(true)
	keyStyle         = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	dateChipStyle    = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorTextTertiary).
				Padding(0, 1)
)

// Bars and containers
var (
	barStyle = lipgloss.NewStyle().
			Background(colorBar).
			Foreground(colorText).
			Padding(0, 1)

	searchBarStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1).
			Margin(0, 1)

	searchBarActiveStyle = searchBarStyle.BorderForeground(colorAccent)

	inputBarStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1).
			Margin(0, 1)
)

// Message bubbles. The border is drawn in the fill color so each bubble
// reads as a solid card with rounded corners.
var (
	receivedBubbleStyle = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorText).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSurface).
				Padding(0, 1)

	sentBubbleStyle = lipgloss.NewStyle().
			Background(colorSent).
			Foreground(colorText).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorSent).
			Padding(0, 1)

	bubbleTimeRecvStyle     = lipgloss.NewStyle().Foreground(colorTextTertiary)
	bubbleTimeSentStyle     = lipgloss.NewStyle().Foreground(colorSentMuted)
	bubbleReactionRecvStyle = lipgloss.NewStyle().Foreground(colorTextSecondary)
	bubbleReactionSentStyle = lipgloss.NewStyle().Foreground(colorText)
)

// Badges and banners
var (
	unreadStyle = lipgloss.NewStyle().
			Background(colorAccentBright).
			Foreground(colorOnAccent).
			Bold(true).
			Padding(0, 1)

	callBannerStyle = lipgloss.NewStyle().
			Background(colorWarning).
			Foreground(colorOnAccent).
			Bold(true).
			Padding(0, 1).
			Margin(0, 1)

	// Avatar and group-sender colors, tuned to stay legible on dark surfaces.
	avatarColors = []lipgloss.Color{
		"#06cf9c", "#53bdeb", "#a791f9", "#ffbc38", "#fc9775", "#ff72a1", "#7f9cf5", "#4fd1c5",
	}
)

// withBackground renders s on bg across its full width, re-applying the
// background after every embedded reset so styled fragments inside the
// line don't punch holes in the fill.
func withBackground(s string, bg lipgloss.Color, width int) string {
	probe := lipgloss.NewStyle().Background(bg).Render("x")
	if prefix, _, ok := strings.Cut(probe, "x"); ok && prefix != "" {
		s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+prefix)
	}
	style := lipgloss.NewStyle().Background(bg)
	if width > 0 {
		style = style.Width(width)
	}
	return style.Render(s)
}
