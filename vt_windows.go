//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// enableVT включает ENABLE_VIRTUAL_TERMINAL_PROCESSING, чтобы ANSI-escape
// (цвета, перемещение курсора) работали в classic-консоли Windows.
func enableVT() {
	const enableVTProcessing = 0x0004
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getStdHandle := kernel32.NewProc("GetStdHandle")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	stdOut := -11 // STD_OUTPUT_HANDLE
	handle, _, _ := getStdHandle.Call(uintptr(stdOut))

	var mode uint32
	getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	setConsoleMode.Call(handle, uintptr(mode|enableVTProcessing))
}
