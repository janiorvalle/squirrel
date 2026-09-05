package tui

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/janiorvalle/squirrel/internal/setup"
)

// wait is the one line on the terminal while setup works between two
// screens: the steps reported since the last screen, "reading repos 37 of
// 143, asking gh about origins 12 of 120", redrawn in place as the counts
// move. It shows once the work has run for `after`, a second on a
// terminal, so a three-repo scan flickers nothing. Whatever comes next
// replaces it: a screen, through clear, or any line setup prints, since
// setup prints through it. A step of one command is named, not counted.
type wait struct {
	out   io.Writer
	after time.Duration
	guard sync.Mutex
	steps []setup.Progress
	timer *time.Timer
	shown bool
}

func newWait(out io.Writer, after time.Duration) *wait {
	return &wait{out: out, after: after}
}

// report is Options.Progress: the step's count so far, kept by name in
// the order the steps began.
func (w *wait) report(step setup.Progress) {
	w.guard.Lock()
	defer w.guard.Unlock()
	if len(w.steps) == 0 {
		w.timer = time.AfterFunc(w.after, w.show)
	}
	index := slices.IndexFunc(w.steps, func(known setup.Progress) bool { return known.Doing == step.Doing })
	if index < 0 {
		w.steps = append(w.steps, step)
	} else {
		w.steps[index] = step
	}
	if w.shown {
		w.draw()
	}
}

// show puts the line up once the work has run long enough to be worth a
// line, unless it ended first.
func (w *wait) show() {
	w.guard.Lock()
	defer w.guard.Unlock()
	if len(w.steps) == 0 {
		return
	}
	w.shown = true
	w.draw()
}

func (w *wait) draw() {
	fmt.Fprint(w.out, "\r\x1b[K"+w.line())
}

func (w *wait) line() string {
	parts := make([]string, 0, len(w.steps))
	for _, step := range w.steps {
		if step.Total > 1 {
			parts = append(parts, fmt.Sprintf("%s %d of %d", step.Doing, step.Done, step.Total))
		} else {
			parts = append(parts, step.Doing)
		}
	}
	return strings.Join(parts, ", ")
}

// clear takes the line off the terminal, or the timer that would have put
// it there, so what comes next starts on an empty line.
func (w *wait) clear() {
	w.guard.Lock()
	defer w.guard.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	if w.shown {
		fmt.Fprint(w.out, "\r\x1b[K")
	}
	w.steps, w.shown = nil, false
}

// Write is what setup prints through: the line goes before anything else
// lands on the terminal.
func (w *wait) Write(text []byte) (int, error) {
	w.clear()
	return w.out.Write(text)
}
