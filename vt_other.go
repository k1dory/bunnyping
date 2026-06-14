//go:build !windows

package main

// На Linux/macOS ANSI работает из коробки — включать ничего не нужно.
func enableVT() {}
