package meta

import (
	"fmt"
	"io"

	"charm.land/lipgloss/v2"
)

// IssuesURL is where users report Violet bugs.
const IssuesURL = "https://github.com/tristanisham/violet/issues"

// CtaFatal prints an error banner, the error, and where to report bugs to w
// (normally stderr). Colors are downsampled to what w supports, so redirected
// output is plain text. It does not exit; main owns the exit status.
func CtaFatal(w io.Writer, err error) {
	banner := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#db0913")).
		Width(10).
		MarginTop(1).
		MarginBottom(1).
		Align(lipgloss.Center)
	// No underline: lipgloss styles underlined text per character, which can stop
	// terminals from detecting the URL as a link.
	link := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0000EE")).
		Bold(true)

	lipgloss.Fprintln(w, banner.Render("Error"))
	fmt.Fprintln(w, err)
	lipgloss.Fprintf(w, "\nIf this looks like a bug, report it at:\n%s\n", link.Render(IssuesURL))
}
