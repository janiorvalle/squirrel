package setup

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// reported is every Progress a session reported, one line each, "reading
// repos 3 of 24", in the order they came.
type reported struct {
	guard sync.Mutex
	lines []string
}

func (r *reported) take(step Progress) {
	r.guard.Lock()
	defer r.guard.Unlock()
	r.lines = append(r.lines, fmt.Sprintf("%s %d of %d", step.Doing, step.Done, step.Total))
}

// climb is a step reported from zero to its total, one at a time.
func climb(doing string, total int) []string {
	var lines []string
	for done := 0; done <= total; done++ {
		lines = append(lines, fmt.Sprintf("%s %d of %d", doing, done, total))
	}
	return lines
}

func TestEveryWaitBeforeAScreenReportsItsStepFromZeroToDone(t *testing.T) {
	home, shell := homeWithManyRepos(t, 24)
	shell.repos = withRepo().repos
	opts, _ := options(t, home, shell, "")
	var seen reported
	opts.Progress = seen.take
	session, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx := context.Background()
	if _, err := session.Gather(ctx, []string{workSkills}); err != nil {
		t.Fatal(err)
	}
	dirs, _ := session.ReposDirs()
	session.Trackers(ctx, dirs)
	session.Tools(ctx)
	var want []string
	want = append(want, climb("cloning "+workSkills, 1)...)
	want = append(want, climb("reading repos", 24)...)
	want = append(want, climb("asking gh about origins", 12)...)
	want = append(want, climb("checking tools", 2)...)
	want = append(want, climb("looking up latest versions", 1)...)
	if got := strings.Join(seen.lines, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("reported:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestAStepWithNothingToDoReportsNothing(t *testing.T) {
	var seen reported
	forEach(context.Background(), counting(seen.take, "reading repos", 0), func(int) {
		t.Fatal("work ran with nothing to do")
	})
	if len(seen.lines) != 0 {
		t.Fatalf("reported %v", seen.lines)
	}
}
