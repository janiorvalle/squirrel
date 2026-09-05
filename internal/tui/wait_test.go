package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/janiorvalle/squirrel/internal/setup"
)

const (
	clearLine = "\r\x1b[K"
	tick      = 10 * time.Millisecond
)

// eventually waits up to a second for out to hold text.
func eventually(t *testing.T, out *bytes.Buffer, text string) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if strings.Contains(out.String(), text) {
			return
		}
	}
	t.Fatalf("no %q in %q", text, out.String())
}

func TestTheLineShowsAfterTheDelayAndRedrawsInPlace(t *testing.T) {
	var out bytes.Buffer
	waiting := newWait(&out, tick)
	waiting.report(setup.Progress{Doing: "reading repos", Done: 0, Total: 5})
	if out.Len() != 0 {
		t.Fatalf("shown before the delay: %q", out.String())
	}
	eventually(t, &out, clearLine+"reading repos 0 of 5")
	waiting.report(setup.Progress{Doing: "reading repos", Done: 3, Total: 5})
	waiting.report(setup.Progress{Doing: "asking gh about origins", Done: 0, Total: 1})
	waiting.clear()
	want := clearLine + "reading repos 0 of 5" + clearLine + "reading repos 3 of 5" + clearLine + "reading repos 3 of 5, asking gh about origins" + clearLine
	if out.String() != want {
		t.Fatalf("drew %q, want %q", out.String(), want)
	}
}

func TestAWaitThatEndsInsideTheDelayShowsNothing(t *testing.T) {
	var out bytes.Buffer
	waiting := newWait(&out, tick)
	waiting.report(setup.Progress{Doing: "reading repos", Done: 0, Total: 3})
	waiting.report(setup.Progress{Doing: "reading repos", Done: 3, Total: 3})
	waiting.clear()
	time.Sleep(5 * tick)
	if out.Len() != 0 {
		t.Fatalf("drew %q", out.String())
	}
}

func TestAPrintThroughTheWaitTakesTheLineOffFirst(t *testing.T) {
	var out bytes.Buffer
	waiting := newWait(&out, tick)
	waiting.report(setup.Progress{Doing: "pulling me/skills", Done: 0, Total: 1})
	eventually(t, &out, clearLine+"pulling me/skills")
	if _, err := waiting.Write([]byte("\nskill repos\n")); err != nil {
		t.Fatal(err)
	}
	if want := clearLine + "pulling me/skills" + clearLine + "\nskill repos\n"; out.String() != want {
		t.Fatalf("drew %q, want %q", out.String(), want)
	}
	waiting.report(setup.Progress{Doing: "checking tools", Done: 1, Total: 4})
	eventually(t, &out, "skill repos\n"+clearLine+"checking tools 1 of 4")
}
