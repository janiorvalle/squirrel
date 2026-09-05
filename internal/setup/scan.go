package setup

import (
	"context"
	"sync"
)

// scanWorkers is how many git or gh commands the tracker scan runs at
// once. A hundred checkouts are five hundred git commands, and eight at a
// time keeps that to a few seconds without forking them all at once on a
// two-core machine.
const scanWorkers = 8

// Progress is one step of a wait between two screens, for a terminal to
// show while nothing else is on it: what setup is doing, how many commands
// the step runs, and how many have finished.
type Progress struct {
	Doing string
	Done  int
	Total int
}

// counted is a step being counted up as its work finishes. Each finish
// goes to Options.Progress, one call at a time, from whichever goroutine
// finished. A step with nothing to do reports nothing, and neither does a
// run with no Progress, the flag path.
type counted struct {
	report func(Progress)
	step   Progress
	guard  sync.Mutex
}

// counting starts a step and reports it at zero, so a wait shows from its
// first moment and not from its first finish.
func counting(report func(Progress), doing string, total int) *counted {
	c := &counted{report: report, step: Progress{Doing: doing, Total: total}}
	c.tell()
	return c
}

func (c *counted) finished() {
	c.guard.Lock()
	defer c.guard.Unlock()
	c.step.Done++
	c.tell()
}

func (c *counted) tell() {
	if c.report != nil && c.step.Total > 0 {
		c.report(c.step)
	}
}

// forEach runs work for every index below the step's total, scanWorkers
// at a time, counting the step up as each finishes, and hands out no more
// indexes once ctx is done. Each call writes its own slot, so a caller
// reads results by index and the input's order holds whatever order the
// calls finish in.
func forEach(ctx context.Context, step *counted, work func(index int)) {
	count := step.step.Total
	indexes := make(chan int)
	var workers sync.WaitGroup
	for range min(count, scanWorkers) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range indexes {
				work(index)
				step.finished()
			}
		}()
	}
	for index := 0; index < count && ctx.Err() == nil; index++ {
		indexes <- index
	}
	close(indexes)
	workers.Wait()
}

// readRepoStates is readRepoState for every repo, in the repos' order.
func readRepoStates(ctx context.Context, opts Options, repos []trackerRepo) []repoState {
	states := make([]repoState, len(repos))
	forEach(ctx, counting(opts.Progress, "reading repos", len(repos)), func(index int) {
		states[index] = readRepoState(ctx, opts, repos[index].dir)
	})
	return states
}

// readOrigins is readOrigin for every URL, in the URLs' order.
func readOrigins(ctx context.Context, opts Options, urls []string) []originFacts {
	facts := make([]originFacts, len(urls))
	forEach(ctx, counting(opts.Progress, "asking gh about origins", len(urls)), func(index int) {
		facts[index] = readOrigin(ctx, opts, urls[index])
	})
	return facts
}
