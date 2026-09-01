//go:build windows

package ui

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// enableVirtualTerminal turns on ANSI escape handling for a Windows console.
// Windows 10 1511 and later support it but do not enable it by default; older
// consoles simply fail the call, and the caller keeps its colours off.
func enableVirtualTerminal(w io.Writer) {
	f, ok := w.(*os.File)
	if !ok {
		return
	}
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
