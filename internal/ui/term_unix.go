//go:build !windows

package ui

import (
	"io"
	"os"

	"golang.org/x/term"
)

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// enableVirtualTerminal is a no-op outside Windows: every terminal that reports
// itself as one here already understands ANSI escapes.
func enableVirtualTerminal(io.Writer) {}
