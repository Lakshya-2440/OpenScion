package ui

import (
	"fmt"
	"strings"
)

// ANSI color codes
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"

	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	BgRed   = "\033[41m"
	BgGreen = "\033[42m"
	BgCyan  = "\033[46m"
	BgGray  = "\033[100m"

	BrightCyan   = "\033[38;5;51m"
	BrightGreen  = "\033[38;5;82m"
	BrightYellow = "\033[38;5;220m"
	BrightPurple = "\033[38;5;141m"
	DarkGray     = "\033[38;5;240m"
	LightGray    = "\033[38;5;250m"
)

// Symbols
const (
	SymCheck   = "✔"
	SymCross   = "✖"
	SymArrow   = "➜"
	SymBullet  = "•"
	SymStar    = "★"
	SymSparkle = "✨"
	SymBranch  = "" // or standard git branch symbol
	SymAgent   = "🤖"
	SymMerge   = "🔀"
)

func PrintSuccess(msg string) {
	fmt.Printf("%s%s %s%s\n", BrightGreen, SymCheck, msg, Reset)
}

func PrintError(msg string) {
	fmt.Printf("%s%s Error: %s%s\n", Red, SymCross, msg, Reset)
}

func PrintInfo(msg string) {
	fmt.Printf("%sℹ %s%s\n", BrightCyan, msg, Reset)
}

func PrintStep(step, total int, msg string) {
	fmt.Printf(" %s[%d/%d]%s %s...\n", DarkGray, step, total, Reset, msg)
}

func PrintStepDone(step, total int, msg string) {
	fmt.Printf(" %s[%d/%d]%s %s %s%s%s\n", DarkGray, step, total, Reset, msg, BrightGreen, SymCheck, Reset)
}

// FormatBadge returns a colored pill badge
func FormatBadge(text, color, bgColor string) string {
	return fmt.Sprintf("%s%s %s %s", color, bgColor, text, Reset)
}

// PrintBox displays a styled card in the terminal
func PrintBox(title string, lines []string) {
	maxLen := len(title) + 4
	for _, l := range lines {
		cleanLen := len(stripANSI(l))
		if cleanLen > maxLen {
			maxLen = cleanLen
		}
	}
	if maxLen < 50 {
		maxLen = 50
	}

	horizontal := strings.Repeat("─", maxLen-len(title)-3)
	fmt.Printf("%s╭─ %s%s%s %s%s╮%s\n", Cyan, Bold, White, title, Cyan, horizontal, Reset)
	for _, l := range lines {
		cleanLen := len(stripANSI(l))
		padding := strings.Repeat(" ", maxLen-cleanLen)
		fmt.Printf("%s│%s %s%s %s│%s\n", Cyan, Reset, l, padding, Cyan, Reset)
	}
	fmt.Printf("%s╰%s╯%s\n", Cyan, strings.Repeat("─", maxLen+2), Reset)
}

func stripANSI(str string) string {
	inEscape := false
	var out []rune
	for _, r := range str {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

