package publish

import (
	"fmt"
	"os"
)

// UI wraps coloured terminal output.
type UI struct {
	Bold   string
	Dim    string
	Reset  string
	Green  string
	Yellow string
	Cyan   string
	Red    string
}

func NewUI() *UI {
	u := &UI{}
	fi, err := os.Stdout.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		u.Bold = "\033[1m"
		u.Dim = "\033[2m"
		u.Reset = "\033[0m"
		u.Green = "\033[32m"
		u.Yellow = "\033[33m"
		u.Cyan = "\033[36m"
		u.Red = "\033[31m"
	}
	return u
}

func (u *UI) Say(msg string) { fmt.Println(msg) }

func (u *UI) Hdr(msg string) {
	fmt.Printf("\n%s== %s ==%s\n", u.Bold+u.Cyan, msg, u.Reset)
}

func (u *UI) Warn(msg string) {
	fmt.Printf("%s! %s%s\n", u.Yellow, msg, u.Reset)
}

func (u *UI) Err(msg string) {
	fmt.Fprintf(os.Stderr, "%sx %s%s\n", u.Red, msg, u.Reset)
}

func (u *UI) Info(msg string) {
	fmt.Printf("%s  %s%s\n", u.Dim, msg, u.Reset)
}

// B wraps s in bold.
func (u *UI) B(s string) string { return u.Bold + s + u.Reset }

// D wraps s in dim.
func (u *UI) D(s string) string { return u.Dim + s + u.Reset }

// G wraps s in green.
func (u *UI) G(s string) string { return u.Green + s + u.Reset }
