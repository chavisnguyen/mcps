package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// A small tick-list with titled sections. It only uses plain ANSI cursor
// movement (no terminal queries), so it works in any terminal.

type pickItem struct {
	ID, Name, Note string
	On             bool
}

type pickSection struct {
	Title string
	Items []pickItem
}

// pickFn is replaceable so tests can drive the flow without a terminal.
var pickFn = runPicker

type row struct {
	header bool
	text   string
	sec    int
	item   int
}

func runPicker(secs []pickSection, footer string) (map[string]bool, error) {
	var rows []row
	var cursors []int // indexes into rows that can hold the cursor
	nameW := 0
	for si, s := range secs {
		rows = append(rows, row{header: true, text: s.Title, sec: si})
		for ii, it := range s.Items {
			nameW = max(nameW, len([]rune(it.Name)))
			cursors = append(cursors, len(rows))
			rows = append(rows, row{sec: si, item: ii})
		}
	}
	if len(cursors) == 0 {
		return map[string]bool{}, nil
	}
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	defer term.Restore(int(os.Stdin.Fd()), old)
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	pos, drawn := 0, 0
	count := func() (n, total int) {
		for _, s := range secs {
			for _, it := range s.Items {
				total++
				if it.On {
					n++
				}
			}
		}
		return
	}
	render := func() {
		w, h, _ := term.GetSize(int(os.Stdout.Fd()))
		if w <= 0 {
			w = 80
		}
		if h <= 0 {
			h = 24
		}
		view := max(h-4, 5)
		cur := cursors[pos]
		start := 0
		if len(rows) > view {
			start = min(max(cur-view/2, 0), len(rows)-view)
		}
		end := min(start+view, len(rows))
		if drawn > 0 {
			fmt.Printf("\x1b[%dA", drawn)
		}
		fmt.Print("\x1b[J")
		lines := 0
		out := func(s string) { fmt.Print(s + "\r\n"); lines++ }
		for i := start; i < end; i++ {
			r := rows[i]
			if r.header {
				if i > start {
					out("")
				}
				out(bold(r.text))
				continue
			}
			it := secs[r.sec].Items[r.item]
			box := "[ ]"
			if it.On {
				box = "[" + green("x") + "]"
			}
			ptr := "  "
			name := fmt.Sprintf("%-*s", nameW, it.Name)
			if i == cur {
				ptr, name = bold("› "), bold(name)
			}
			note := trunc(it.Note, max(w-nameW-9, 10))
			out(ptr + box + " " + name + "  " + dim(note))
		}
		n, total := count()
		out("")
		out(dim(fmt.Sprintf("%d of %d on · %s", n, total, footer)))
		drawn = lines
	}

	buf := make([]byte, 8)
	for {
		render()
		k, err := os.Stdin.Read(buf)
		if err != nil || k == 0 {
			return nil, errors.New("could not read the keyboard")
		}
		key := string(buf[:k])
		r := rows[cursors[pos]]
		switch {
		case key == "\x03" || key == "q":
			fmt.Print("\x1b[J")
			return nil, errors.New("cancelled")
		case key == "\r" || key == "\n":
			fmt.Printf("\x1b[%dA\x1b[J", drawn)
			res := map[string]bool{}
			for _, s := range secs {
				for _, it := range s.Items {
					res[it.ID] = it.On
				}
			}
			return res, nil
		case key == "\x1b[A" || key == "k":
			pos = max(pos-1, 0)
		case key == "\x1b[B" || key == "j":
			pos = min(pos+1, len(cursors)-1)
		case key == " ":
			it := &secs[r.sec].Items[r.item]
			it.On = !it.On
		case key == "a" || key == "\x1b[C":
			setAll(secs, true)
		case key == "n" || key == "\x1b[D":
			setAll(secs, false)
		}
	}
}

func setAll(secs []pickSection, on bool) {
	for si := range secs {
		for ii := range secs[si].Items {
			secs[si].Items[ii].On = on
		}
	}
}

var _ = strings.TrimSpace
