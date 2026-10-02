package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procGetConsoleMode        = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode        = kernel32.NewProc("SetConsoleMode")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
	enableVirtualTerminalFlag = uint32(0x0004)
)

// launchedFromExplorer reports whether this process owns its console window,
// i.e. it was double-clicked rather than run from a terminal. The window
// would otherwise close before the user can read the results.
func launchedFromExplorer() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	return n == 1
}

// enableColor turns on ANSI escape handling for the Windows console.
func enableColor() bool {
	h := uintptr(syscall.Handle(os.Stdout.Fd()))
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	r, _, _ := procSetConsoleMode.Call(h, uintptr(mode|enableVirtualTerminalFlag))
	return r != 0
}

// detachConsole lets go of the console so a double-clicked tray app does not
// leave an empty window open.
func detachConsole() { procFreeConsole.Call() }
