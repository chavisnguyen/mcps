package main

import (
	"os"

	"golang.org/x/term"
)

// Colour only when writing to a terminal and NO_COLOR is not set.
var useColor = os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func red(s string) string    { return paint("31", s) }
func dim(s string) string    { return paint("2", s) }
func bold(s string) string   { return paint("1", s) }
