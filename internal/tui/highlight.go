package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	numColor  = lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#67E8F9"}
	jsonKeyS  = lipgloss.NewStyle().Foreground(accent)
	jsonStrS  = lipgloss.NewStyle().Foreground(okColor)
	jsonNumS  = lipgloss.NewStyle().Foreground(numColor)
	jsonBoolS = lipgloss.NewStyle().Foreground(warnColor)
	commentS  = lipgloss.NewStyle().Foreground(subtle).Italic(true)

	h2S       = lipgloss.NewStyle().Bold(true).Foreground(accent)
	h3S       = lipgloss.NewStyle().Bold(true)
	codeSpanS = lipgloss.NewStyle().Foreground(numColor)
	labelS    = lipgloss.NewStyle().Foreground(subtle)
)

var jsonTokenRe = regexp.MustCompile(
	`"(?:\\.|[^"\\])*"\s*:|"(?:\\.|[^"\\])*"|-?\d+(?:\.\d+)?|\btrue\b|\bfalse\b|\bnull\b`)

// highlightJSON colorizes keys, strings, numbers, and booleans in a JSON
// document. Comment lines (starting with #) are dimmed.
func highlightJSON(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[i] = commentS.Render(line)
			continue
		}
		lines[i] = jsonTokenRe.ReplaceAllStringFunc(line, func(tok string) string {
			switch {
			case strings.HasSuffix(tok, ":"):
				return jsonKeyS.Render(tok)
			case strings.HasPrefix(tok, `"`):
				return jsonStrS.Render(tok)
			case tok == "true" || tok == "false" || tok == "null":
				return jsonBoolS.Render(tok)
			default:
				return jsonNumS.Render(tok)
			}
		})
	}
	return strings.Join(lines, "\n")
}

var (
	statusRe   = regexp.MustCompile(`^### \[([^\]]+)\]`)
	codeSpanRe = regexp.MustCompile("`[^`]+`")
	bulletRe   = regexp.MustCompile(`^- ([a-z ()]+):`)
)

// highlightReport colorizes the markdown-ish run report: run headers,
// per-item status headers, bullet labels, and inline code spans.
func highlightReport(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "## "):
			lines[i] = h2S.Render(line)
		case strings.HasPrefix(line, "### "):
			lines[i] = renderItemHeader(line)
		case strings.HasPrefix(line, "- "):
			if m := bulletRe.FindStringSubmatch(line); m != nil {
				rest := line[len(m[0]):]
				if m[1] == "error" {
					rest = flashS.Render(rest)
				} else {
					rest = codeSpanRe.ReplaceAllStringFunc(rest, renderCodeSpan)
				}
				lines[i] = labelS.Render("- "+m[1]+":") + rest
			} else {
				lines[i] = codeSpanRe.ReplaceAllStringFunc(line, renderCodeSpan)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// renderCodeSpan adapts the variadic lipgloss Render to the
// func(string) string shape regexp.ReplaceAllStringFunc wants.
func renderCodeSpan(s string) string {
	return codeSpanS.Render(s)
}

func renderItemHeader(line string) string {
	m := statusRe.FindStringSubmatch(line)
	if m == nil {
		return h3S.Render(line)
	}
	color := warnColor
	switch m[1] {
	case "done":
		color = okColor
	case "needs_attention":
		color = errColor
	}
	badge := lipgloss.NewStyle().Bold(true).Foreground(color).Render("[" + m[1] + "]")
	return h3S.Render("### ") + badge + h3S.Render(line[len(m[0]):])
}
