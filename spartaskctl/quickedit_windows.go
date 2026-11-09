package main

import (
	"os"
	"syscall"
)

// Console input modes (wincon.h). QuickEdit lets a click select text in the console window, and
// while a selection is open Windows suspends every program writing to that console.
const (
	enableQuickEditMode = 0x0040
	enableExtendedFlags = 0x0080
)

var procSetConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// disableQuickEdit turns QuickEdit off for this console while spartaskctl runs, so a click in the
// window cannot stop the installation, and returns the function that restores the previous mode.
// Without a console (input redirected) there is nothing to change.
func disableQuickEdit() func() {
	console := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(console, &mode); err != nil {
		return func() {}
	}
	if ok, _, _ := procSetConsoleMode.Call(uintptr(console), uintptr((mode|enableExtendedFlags)&^enableQuickEditMode)); ok == 0 {
		return func() {}
	}
	return func() {
		// Best effort: the console's own mode is restored when the window closes at the latest.
		procSetConsoleMode.Call(uintptr(console), uintptr(mode))
	}
}
