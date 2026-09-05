// Package tui is setup on a terminal: one screen per question on charm's
// huh, saved answers preselected, a plan and a confirm at the end. It
// collects answers and renders; every rule, the plan, and the apply stay in
// the setup package.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/janiorvalle/squirrel/internal/setup"
	"github.com/janiorvalle/squirrel/internal/skills"
)

// ErrQuit is returned when the person leaves before the plan is applied:
// Ctrl-C anywhere, or Esc on the first screen.
var ErrQuit = errors.New("quit")

// Run asks each question in turn, prints the plan, and applies it after
// the confirm. Nothing in a harness or a repo of the person's changes
// before that; the skills repo clone under ~/.squirrel/repos is synced as
// soon as it's named, since its collisions are the next question.
func Run(ctx context.Context, opts setup.Options) error {
	waiting := newWait(opts.Stdout, time.Second)
	show := terminal(opts.Stdin, opts.Stdout, waiting)
	opts.Progress, opts.Stdout = waiting.report, waiting
	session, err := setup.Start(opts)
	if err != nil {
		return err
	}
	defer session.Close()
	return run(ctx, session, opts, show)
}

// direction is which way a step is entered: forward from the one before
// it, or backward from the one after, when Esc walks into a list of
// questions and the last one should show.
type direction int

const (
	forward direction = iota
	backward
)

// step is one question, or a list of them, or a computation between two
// questions. skipped means nothing was shown, so Esc walks over it.
type step func(direction) (outcome, error)

const skipped outcome = -1

// sequence runs steps front to back, or back to front when entered
// backward. back from a step runs the one before it; back off the front
// is back for the whole list.
func sequence(steps ...step) step {
	return func(entered direction) (outcome, error) {
		index, heading, shown := 0, forward, false
		if entered == backward {
			index, heading = len(steps)-1, backward
		}
		for index >= 0 && index < len(steps) {
			result, err := steps[index](heading)
			if err != nil {
				return result, err
			}
			switch result {
			case quit:
				return quit, nil
			case next:
				shown, heading = true, forward
			case back:
				heading = backward
			}
			if heading == forward {
				index++
			} else {
				index--
			}
		}
		if index < 0 {
			return back, nil
		}
		if !shown {
			return skipped, nil
		}
		return next, nil
	}
}

// flow is the run in progress: the session the facts come from, and the
// answers so far, kept across screens so Esc shows a question with the
// answer it had.
type flow struct {
	ctx     context.Context
	session *setup.Session
	opts    setup.Options
	out     io.Writer
	show    runner

	harnesses     []string
	harnessesInit bool
	skillRepo     string
	collisions    []collisionScratch
	reposPick     string
	reposTyped    string
	questions     []setup.TrackerQuestion
	trackers      []backendScratch
	repoPaths     bool
	openPRs       bool
	tools         []string
	toolsInit     bool
	plan          *setup.Plan
	answers       setup.Answers
	confirmed     bool
}

type collisionScratch struct {
	name    string
	sources []string
	pick    string
}

// backendScratch is one backend's answers as the screens collect them: a
// round per key typed, in the order they were typed, since a person's
// repos sit in several teams. A backend that takes no key, or has a
// default for the one thing it takes, has the one round.
type backendScratch struct {
	backend setup.Backend
	rounds  []roundScratch
}

// roundScratch is one key and the checkouts checked for it. shown is
// whether its list came up yet, since the origin's guesses come checked
// the first time only.
type roundScratch struct {
	key   string
	dirs  []string
	shown bool
}

// perKey reports whether a backend is asked in rounds: it takes a key and
// has no default for it, so each key is a team of the person's own and the repos in it are its own list.
func perKey(chosen setup.Backend) bool {
	return chosen.Argument != "" && chosen.Default == ""
}

const elsewhere = "elsewhere"

func run(ctx context.Context, session *setup.Session, opts setup.Options, show runner) error {
	f := &flow{ctx: ctx, session: session, opts: opts, out: opts.Stdout, show: show}
	result, err := sequence(
		f.harnessScreen,
		f.skillRepoScreen,
		f.collisionScreens,
		f.reposDirScreen,
		f.reposTypedScreen,
		f.trackerScreens,
		f.toolsScreen,
		f.planScreen,
		f.pullRequestScreen,
		f.applyScreen,
	)(forward)
	if err != nil {
		return err
	}
	if result == quit || result == back {
		return ErrQuit
	}
	if !f.confirmed {
		fmt.Fprintln(f.out, "Nothing changed in the harnesses.")
		return nil
	}
	return f.session.Apply(f.ctx, f.plan, f.answers)
}

// ask runs one field as a screen and reads how it ended.
func (f *flow) ask(field huh.Field, summary func() string) (outcome, error) {
	return f.present(newScreen(field, summary))
}

// present runs one screen and reads how it ended.
func (f *flow) present(s *screen) (outcome, error) {
	if err := f.show(s); err != nil {
		return quit, fmt.Errorf("[SQUIRREL-TUI] the terminal stopped answering: %w; rerun with --yes and --harness to skip the questions", err)
	}
	return s.outcome, nil
}

// harnessScreen is every run: the checkbox list of harnesses, saved picks
// and newly found ones checked. One Enter when nothing changed.
func (f *flow) harnessScreen(direction) (outcome, error) {
	choices, err := f.session.Harnesses()
	if err != nil {
		return quit, err
	}
	names := map[string]string{}
	options := make([]huh.Option[string], 0, len(choices))
	for _, choice := range choices {
		names[choice.Key] = choice.Name
		state := "not found"
		if choice.Found {
			state = "found"
		}
		options = append(options, huh.NewOption(fmt.Sprintf("%-11s %s, %s", choice.Name, choice.Where, state), choice.Key))
		if !f.harnessesInit && choice.Checked {
			f.harnesses = append(f.harnesses, choice.Key)
		}
	}
	f.harnessesInit = true
	field := huh.NewMultiSelect[string]().
		Title("Install into which harnesses?").
		Description("Space toggles, Enter continues.").
		Options(options...).
		Filterable(false).
		Value(&f.harnesses).
		Validate(func(picked []string) error {
			if len(picked) == 0 {
				return errors.New("pick at least one harness")
			}
			return nil
		})
	return f.ask(field, func() string {
		picked := make([]string, 0, len(f.harnesses))
		for _, key := range f.harnesses {
			picked = append(picked, names[key])
		}
		return "harnesses  " + strings.Join(picked, ", ")
	})
}

// skillRepoScreen asks once for a skills repo of the person's own.
func (f *flow) skillRepoScreen(direction) (outcome, error) {
	if f.session.SkillRepoAsked() {
		return skipped, nil
	}
	field := huh.NewInput().
		Title("Do you have a skills repo of your own?").
		Description("owner/name on GitHub, with a skills/ folder holding one folder per skill. Enter with nothing to skip.").
		Placeholder("owner/name").
		Value(&f.skillRepo).
		Validate(func(typed string) error {
			if strings.TrimSpace(typed) == "" {
				return nil
			}
			_, err := setup.RepoName(typed)
			return err
		})
	return f.ask(field, func() string {
		if strings.TrimSpace(f.skillRepo) == "" {
			return "skills repo  none"
		}
		name, _ := setup.RepoName(f.skillRepo)
		return "skills repo  " + name
	})
}

// repoNames is the repos this run installs from: the saved and flagged
// ones, plus the one typed.
func (f *flow) repoNames() ([]string, error) {
	names, err := f.session.SkillRepos()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(f.skillRepo) != "" {
		name, err := setup.RepoName(f.skillRepo)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// collisionScreens fetches the repos, then asks about each skill name in
// more than one source that no flag or saved pick settles.
func (f *flow) collisionScreens(entered direction) (outcome, error) {
	names, err := f.repoNames()
	if err != nil {
		return quit, err
	}
	open, err := f.session.Gather(f.ctx, names)
	if err != nil {
		return quit, err
	}
	if !sameCollisions(f.collisions, open) {
		f.collisions = nil
		for _, collision := range open {
			f.collisions = append(f.collisions, collisionScratch{name: collision.Name, sources: collision.Sources})
		}
	}
	steps := make([]step, 0, len(f.collisions))
	for index := range f.collisions {
		steps = append(steps, f.collisionScreen(&f.collisions[index]))
	}
	result, err := sequence(steps...)(entered)
	if err != nil || result != next && result != skipped {
		return result, err
	}
	picks := map[string]string{}
	for _, collision := range f.collisions {
		picks[collision.name] = collision.pick
	}
	if err := f.session.PickSources(picks); err != nil {
		return quit, err
	}
	return result, nil
}

// sameCollisions reports whether the scratch answers are for these very
// collisions, by name and sources, so a repo swapped after Esc gets fresh
// questions.
func sameCollisions(scratch []collisionScratch, open []skills.Collision) bool {
	if len(scratch) != len(open) {
		return false
	}
	for index, collision := range open {
		if scratch[index].name != collision.Name || strings.Join(scratch[index].sources, ",") != strings.Join(collision.Sources, ",") {
			return false
		}
	}
	return true
}

func (f *flow) collisionScreen(collision *collisionScratch) step {
	return func(direction) (outcome, error) {
		options := make([]huh.Option[string], 0, len(collision.sources)+1)
		for _, source := range collision.sources {
			options = append(options, huh.NewOption(setup.UseWording(source), source))
		}
		options = append(options, huh.NewOption("rename it yourself", setup.Rename))
		field := huh.NewSelect[string]().
			Title(fmt.Sprintf("Skill %q is in %s. Which one goes into the harnesses?", collision.name, strings.Join(collision.sources, " and "))).
			Options(options...).
			Value(&collision.pick)
		return f.ask(field, func() string {
			if collision.pick == setup.Rename {
				return collision.name + "  rename it yourself"
			}
			return collision.name + "  " + setup.UseWording(collision.pick)
		})
	}
}

// reposDirScreen asks once where the repos live: a folder found under
// home, or somewhere else, which the next screen takes typed.
func (f *flow) reposDirScreen(direction) (outcome, error) {
	if f.session.ReposDirsAsked() {
		return skipped, nil
	}
	guesses := f.session.ReposDirGuesses()
	if len(guesses) == 0 {
		f.reposPick = elsewhere
		return skipped, nil
	}
	if f.reposPick == "" {
		f.reposPick = guesses[0]
	}
	options := make([]huh.Option[string], 0, len(guesses)+1)
	for _, guess := range guesses {
		options = append(options, huh.NewOption(setup.Display(f.opts.Home, guess), guess))
	}
	options = append(options, huh.NewOption("somewhere else", elsewhere))
	field := huh.NewSelect[string]().
		Title("Where do your repos live?").
		Description("A folder with a git checkout in each subfolder. Setup reads each repo's Tracker line there.").
		Options(options...).
		Value(&f.reposPick)
	return f.ask(field, func() string {
		if f.reposPick == elsewhere {
			return "repos  somewhere else"
		}
		return "repos  " + setup.Display(f.opts.Home, f.reposPick)
	})
}

// reposTypedScreen takes the folder typed when no guess fit.
func (f *flow) reposTypedScreen(direction) (outcome, error) {
	if f.session.ReposDirsAsked() || f.reposPick != elsewhere {
		return skipped, nil
	}
	field := huh.NewInput().
		Title("Where do your repos live?").
		Description("A path, comma separated for more than one. Enter with nothing to skip.").
		Placeholder("~/code").
		Value(&f.reposTyped).
		Validate(func(typed string) error {
			_, err := setup.ParseReposDirs(typed, f.opts.Home)
			return err
		})
	return f.ask(field, func() string {
		dirs, _ := setup.ParseReposDirs(f.reposTyped, f.opts.Home)
		if len(dirs) == 0 {
			return "repos  none"
		}
		return "repos  " + shownList(f.opts.Home, dirs)
	})
}

// reposDirs is the folders this run scans: the saved and flagged ones,
// plus the answer.
func (f *flow) reposDirs() ([]string, bool, error) {
	dirs, err := f.session.ReposDirs()
	if err != nil {
		return nil, false, err
	}
	if f.session.ReposDirsAsked() {
		return dirs, true, nil
	}
	if f.reposPick == elsewhere {
		typed, err := setup.ParseReposDirs(f.reposTyped, f.opts.Home)
		if err != nil {
			return nil, false, err
		}
		return append(dirs, typed...), true, nil
	}
	return append(dirs, f.reposPick), true, nil
}

// trackerScreens asks about every repo that declares no tracker, one
// backend at a time in the order the backends come, over the repos no
// earlier screen took. Linear asks in rounds: the team key, the repos in
// that team, then the key again until Enter leaves it empty. GitHub Issues
// and markdown tasks are one list each. Unchecked on every
// screen means skip, and the next run remembers it. A screen with no
// repo left to offer is not shown.
func (f *flow) trackerScreens(entered direction) (outcome, error) {
	dirs, _, err := f.reposDirs()
	if err != nil {
		return quit, err
	}
	questions := f.session.Trackers(f.ctx, dirs)
	if !sameQuestions(f.questions, questions) {
		f.questions = questions
		f.trackers = nil
		for _, backend := range setup.Backends() {
			f.trackers = append(f.trackers, backendScratch{backend: backend, rounds: []roundScratch{{}}})
		}
	}
	f.repoPaths = len(dirs) > 1
	steps := make([]step, 0, len(f.trackers))
	for index := range f.trackers {
		if perKey(f.trackers[index].backend) {
			steps = append(steps, f.keyRounds(index))
		} else {
			steps = append(steps, f.backendScreen(index))
		}
	}
	return sequence(steps...)(entered)
}

func sameQuestions(known, questions []setup.TrackerQuestion) bool {
	if len(known) != len(questions) {
		return false
	}
	for index := range questions {
		if known[index] != questions[index] {
			return false
		}
	}
	return true
}

// offered is the repos a round shows: every one no earlier backend's
// rounds and no earlier round of its own took.
func (f *flow) offered(index, round int) []setup.TrackerQuestion {
	taken := map[string]bool{}
	for backendIndex, scratch := range f.trackers[:index+1] {
		rounds := scratch.rounds
		if backendIndex == index {
			rounds = rounds[:round]
		}
		for _, earlier := range rounds {
			for _, dir := range earlier.dirs {
				taken[dir] = true
			}
		}
	}
	var offered []setup.TrackerQuestion
	for _, question := range f.questions {
		if !taken[question.Dir] {
			offered = append(offered, question)
		}
	}
	return offered
}

// repoLabel is a repo's row: its name, or its path when more than one
// repos folder is scanned and two could share a name.
func (f *flow) repoLabel(question setup.TrackerQuestion) string {
	if f.repoPaths {
		return setup.Display(f.opts.Home, question.Dir)
	}
	return question.Repo
}

// keyRounds is a backend asked per key. A round is its key screen then
// the list of repos in that team; each key answered adds the next round,
// and the backend ends on Enter with the key empty or when its list took
// the last repo. Esc walks the rounds back. The backend's line is left
// behind once, when it ends, so the terminal reads one line per backend.
func (f *flow) keyRounds(index int) step {
	current := &f.trackers[index]
	return func(entered direction) (outcome, error) {
		round, heading, shown := 0, forward, false
		if entered == backward {
			round, heading = len(current.rounds)-1, backward
		}
		for round >= 0 {
			result, err := sequence(f.keyScreen(index, round), f.listScreen(index, round))(heading)
			if err != nil {
				return result, err
			}
			switch result {
			case quit:
				return quit, nil
			case next:
				shown, heading = true, forward
			case back:
				heading = backward
			}
			if heading == backward {
				round--
				continue
			}
			if result == skipped || current.rounds[round].key == "" {
				current.rounds[round].dirs = nil
				current.rounds = current.rounds[:round+1]
				break
			}
			if round == len(current.rounds)-1 {
				current.rounds = append(current.rounds, roundScratch{})
			}
			round++
		}
		if round < 0 {
			return back, nil
		}
		if !shown {
			return skipped, nil
		}
		fmt.Fprintln(f.out, current.summary())
		return next, nil
	}
}

// keyScreen is one round's key, Enter for none. Not shown when no repo is
// left to give it.
func (f *flow) keyScreen(index, round int) step {
	current := &f.trackers[index]
	return func(direction) (outcome, error) {
		if len(f.offered(index, round)) == 0 {
			return skipped, nil
		}
		chosen := current.backend
		title := chosen.Label + " " + chosen.Argument + ", Enter for none"
		if round > 0 {
			title = "Another " + title
		}
		field := huh.NewInput().
			Title(title).
			Description("for example " + chosen.Example + ". The repos in it come next, then this screen again for another.").
			Placeholder(chosen.Example).
			Value(&current.rounds[round].key).
			Validate(oneWord(chosen))
		return f.ask(field, noLine)
	}
}

// listScreen is one round's checkbox list, the repos in the team its key
// names. Not shown for an empty key.
func (f *flow) listScreen(index, round int) step {
	current := &f.trackers[index]
	return func(direction) (outcome, error) {
		offered := f.offered(index, round)
		key := current.rounds[round].key
		if key == "" || len(offered) == 0 {
			return skipped, nil
		}
		chosen := current.backend
		title := fmt.Sprintf("Which repos track their work in %s %s %s?", chosen.Label, strings.TrimSuffix(chosen.Argument, " key"), key)
		return f.ask(f.repoList(chosen, &current.rounds[round], offered, title), noLine)
	}
}

// noLine is the summary of a screen that leaves nothing behind: a round's
// screens, whose backend leaves its line when it ends.
func noLine() string {
	return ""
}

// backendScreen is a backend asked once: its checkbox list, with a second
// page for the one thing it needs when any repo is checked.
func (f *flow) backendScreen(index int) step {
	current := &f.trackers[index]
	return func(direction) (outcome, error) {
		offered := f.offered(index, 0)
		if len(offered) == 0 {
			return skipped, nil
		}
		round := &current.rounds[0]
		list := f.repoList(current.backend, round, offered, fmt.Sprintf("Which repos track their work in %s?", current.backend.Label))
		groups := []*huh.Group{huh.NewGroup(list)}
		if current.backend.Argument != "" {
			groups = append(groups, huh.NewGroup(argumentField(current.backend, round)).WithHideFunc(func() bool { return len(round.dirs) == 0 }))
		}
		return f.present(newPages(current.summary, groups...))
	}
}

// repoList is the checkbox list over the repos offered. The first time it
// comes up, the repos whose origin suggests this backend come checked.
func (f *flow) repoList(chosen setup.Backend, round *roundScratch, offered []setup.TrackerQuestion, title string) huh.Field {
	guessed := 0
	for _, question := range offered {
		if question.Guess == chosen.Key {
			guessed++
			if !round.shown {
				round.dirs = append(round.dirs, question.Dir)
			}
		}
	}
	round.shown = true
	options := make([]huh.Option[string], 0, len(offered))
	for _, question := range offered {
		options = append(options, huh.NewOption(f.repoLabel(question), question.Dir))
	}
	description := "Space toggles, Enter continues, / filters by name. Unchecked on every screen means skip."
	if guessed > 0 {
		description += "\n" + countOf(guessed, "repo") + " checked already, from the origin."
	}
	return huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(options...).
		Value(&round.dirs)
}

// argumentField takes the one thing a backend with a default needs, once
// for every repo checked on its list.
func argumentField(chosen setup.Backend, round *roundScratch) huh.Field {
	return huh.NewInput().
		Title(chosen.Label + " " + chosen.Argument + ", the same for every repo checked").
		Description("Enter for " + chosen.Default).
		Placeholder(chosen.Example).
		Value(&round.key).
		Validate(oneWord(chosen))
}

// oneWord rejects a key or folder with a space in it, since the tracker
// line is read by words.
func oneWord(chosen setup.Backend) func(string) error {
	return func(typed string) error {
		if strings.ContainsAny(typed, " \t") {
			return fmt.Errorf("the %s is one word, for example %s", chosen.Argument, chosen.Example)
		}
		return nil
	}
}

// summary is the line a backend leaves behind: each line its rounds
// decided and how many repos get it, the same key twice counted as one.
func (s *backendScratch) summary() string {
	var lines []string
	counts := map[string]int{}
	for _, round := range s.rounds {
		if len(round.dirs) == 0 {
			continue
		}
		line := strings.TrimPrefix(lineFor(s.backend, round.key), "Tracker: ")
		if counts[line] == 0 {
			lines = append(lines, line)
		}
		counts[line] += len(round.dirs)
	}
	if len(lines) == 0 {
		return s.backend.Key + "  none"
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, line+"  "+countOf(counts[line], "repo"))
	}
	return strings.Join(parts, ", ")
}

func countOf(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// trackerAnswers is what the screens decided, one per undeclared repo:
// the line of the first round that has it checked, or skip when none
// does, and the PR when the one PR question said yes and the repo can
// take one.
func (f *flow) trackerAnswers() []setup.TrackerAnswer {
	lines := map[string]string{}
	for _, scratch := range f.trackers {
		for _, round := range scratch.rounds {
			for _, dir := range round.dirs {
				if _, taken := lines[dir]; !taken {
					lines[dir] = lineFor(scratch.backend, round.key)
				}
			}
		}
	}
	var answers []setup.TrackerAnswer
	for _, question := range f.questions {
		line, checked := lines[question.Dir]
		answers = append(answers, setup.TrackerAnswer{Dir: question.Dir, Repo: question.Repo, Line: line, Skip: !checked, OpenPR: checked && f.openPRs && question.PRHold == ""})
	}
	return answers
}

// lineFor is the tracker line for a backend and what was typed for it: a
// backend that takes nothing ignores the text, one that takes something
// gets its default for nothing.
func lineFor(chosen setup.Backend, typed string) string {
	if chosen.Argument == "" {
		return setup.TrackerLine(chosen, "")
	}
	if typed == "" {
		return setup.TrackerLine(chosen, chosen.Default)
	}
	return setup.TrackerLine(chosen, typed)
}

// toolsScreen is the checkbox list of tools setup could install or update.
// Unchecked means skip.
func (f *flow) toolsScreen(direction) (outcome, error) {
	choices := f.session.Tools(f.ctx)
	var options []huh.Option[string]
	var states []string
	for _, choice := range choices {
		if !choice.Actionable {
			states = append(states, choice.State)
			continue
		}
		options = append(options, huh.NewOption(choice.Label, choice.Title))
		if !f.toolsInit && choice.Checked {
			f.tools = append(f.tools, choice.Title)
		}
	}
	f.toolsInit = true
	if len(options) == 0 {
		return skipped, nil
	}
	description := "Space toggles, Enter continues. Unchecked is skipped."
	if len(states) > 0 {
		description += "\n" + strings.Join(states, "\n")
	}
	field := huh.NewMultiSelect[string]().
		Title("Which tools?").
		Description(description).
		Options(options...).
		Filterable(false).
		Value(&f.tools)
	return f.ask(field, func() string {
		if len(f.tools) == 0 {
			return "tools  none"
		}
		var verbs []string
		for _, choice := range choices {
			if verb, _, ok := strings.Cut(choice.Label, ":"); ok && contains(f.tools, choice.Title) {
				verbs = append(verbs, verb)
			}
		}
		return "tools  " + strings.Join(verbs, ", ")
	})
}

// planScreen prints the plan, the last thing before the confirms. Walked
// into backward it shows nothing, so Esc from a confirm lands on the
// screen before the plan.
func (f *flow) planScreen(entered direction) (outcome, error) {
	if entered == backward {
		return skipped, nil
	}
	answers, err := f.collect()
	if err != nil {
		return quit, err
	}
	plan, err := f.session.Plan(f.ctx, answers)
	if err != nil {
		return quit, err
	}
	f.plan, f.answers = plan, answers
	plan.Print(f.out, f.opts, answers)
	if !plan.Pending() {
		fmt.Fprintln(f.out, "\nEverything is in place. Nothing to apply.")
	}
	return skipped, nil
}

// pullRequestScreen asks once whether every line a PR can carry gets one.
// The plan above names each repo that can and why the rest can't.
func (f *flow) pullRequestScreen(direction) (outcome, error) {
	holds := map[string]string{}
	for _, question := range f.questions {
		holds[question.Dir] = question.PRHold
	}
	possible := 0
	for _, answer := range f.answers.Trackers {
		if !answer.Skip && holds[answer.Dir] == "" {
			possible++
		}
	}
	if possible == 0 {
		return skipped, nil
	}
	field := huh.NewConfirm().
		Title(fmt.Sprintf("Open %s?", countOf(possible, "pull request"))).
		Description("Each on branch tracker-line, commit \"docs: name the tracker\", through gh. No writes the lines and leaves them uncommitted.").
		Affirmative("Yes").
		Negative("No").
		Value(&f.openPRs)
	result, err := f.ask(field, func() string {
		if f.openPRs {
			return "pull requests  yes"
		}
		return "pull requests  no, lines left uncommitted"
	})
	f.answers.Trackers = f.trackerAnswers()
	return result, err
}

// applyScreen is the one confirm. With nothing to apply there is nothing
// to confirm, and the run goes straight to the report.
func (f *flow) applyScreen(direction) (outcome, error) {
	f.confirmed = true
	if !f.plan.Pending() {
		return skipped, nil
	}
	field := huh.NewConfirm().
		Title("Apply?").
		Description("Everything above happens now.").
		Affirmative("Yes").
		Negative("No").
		Value(&f.confirmed)
	return f.ask(field, func() string {
		if f.confirmed {
			return "apply  yes"
		}
		return "apply  no"
	})
}

// collect is every answer in the values the plan takes.
func (f *flow) collect() (setup.Answers, error) {
	names, err := f.repoNames()
	if err != nil {
		return setup.Answers{}, err
	}
	dirs, asked, err := f.reposDirs()
	if err != nil {
		return setup.Answers{}, err
	}
	answers := setup.Answers{
		Harnesses:      f.harnesses,
		SkillRepos:     names,
		SkillRepoAsked: true,
		ReposDirs:      dirs,
		ReposDirsAsked: asked,
		Tools:          map[string]bool{},
		Trackers:       f.trackerAnswers(),
	}
	for _, title := range f.tools {
		answers.Tools[title] = true
	}
	return answers, nil
}

func contains(list []string, item string) bool {
	for _, candidate := range list {
		if candidate == item {
			return true
		}
	}
	return false
}

func shownList(home string, paths []string) string {
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		parts = append(parts, setup.Display(home, path))
	}
	return strings.Join(parts, ", ")
}
