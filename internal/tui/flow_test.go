package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/janiorvalle/squirrel/internal/setup"
	"github.com/janiorvalle/squirrel/internal/tools"
)

// fixture is a small squirrel: one skill, the letter, roast with a fixture
// check line and TruffleHog with a real one, so the tools screen can show
// both the install line and the update line by owner.
func fixture() fstest.MapFS {
	return fstest.MapFS{
		"skills/voice/SKILL.md":    {Data: []byte("voice v2\n")},
		"AGENTS.md":                {Data: []byte("# the letter\n")},
		"vendor.json":              {Data: []byte(`{"skills":[]}`)},
		"scripts/install-tool.ps1": {Data: []byte("# installs a tool\n")},
		"tools.md": {Data: []byte("# Tools\n\n" +
			"## git\n\n- Check: `check-git`\n\n" +
			"## roast\n\n- Repo: https://github.com/x/roast\n- Check: `check-roast`\n- Version: `version-roast`\n- Install: `curl roast | sh`\n\n" +
			"## TruffleHog\n\n- Repo: https://github.com/x/trufflehog\n- Check: `command -v trufflehog`\n- Version: `trufflehog --version`\n- Install: `curl trufflehog | sh`\n")},
	}
}

// fakeShell is the machine: which checks pass, what commands print, and
// which repos gh can clone. It answers git the way a clean checkout on
// main at its remote does, so a tracker PR is offered, and gh the way it
// does for a GitHub repo with issues on, so GitHub Issues is guessed.
type fakeShell struct {
	present  map[string]bool
	versions map[string]string
	repos    map[string]map[string]string
	commands []string
	guard    sync.Mutex
	// delay holds each command open, so a scan runs long enough for its
	// line to show.
	delay time.Duration
}

func (f *fakeShell) run(_ context.Context, command string, out io.Writer) error {
	f.guard.Lock()
	f.commands = append(f.commands, command)
	f.guard.Unlock()
	time.Sleep(f.delay)
	if strings.HasPrefix(command, "gh repo clone ") {
		dir := strings.Split(command, "'")[1]
		name := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(dir)), filepath.Base(dir)))
		files, ok := f.repos[name]
		if !ok {
			return errors.New("exit status 1")
		}
		for path, content := range files {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
				return err
			}
		}
		return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	}
	if output, ok := f.versions[command]; ok {
		fmt.Fprintln(out, output)
		return nil
	}
	if strings.HasPrefix(command, "check-") || strings.HasPrefix(command, "command -v ") {
		if f.present[command] {
			return nil
		}
		return errors.New("exit status 1")
	}
	if command == "curl roast | sh" {
		f.present["check-roast"] = true
		f.versions["version-roast"] = "roast 1.1.0"
	}
	if command == "brew upgrade trufflehog" {
		f.versions["trufflehog --version"] = "trufflehog 3.97.4"
	}
	switch {
	case strings.HasPrefix(command, "gh repo view --json hasIssuesEnabled "):
		fmt.Fprintln(out, `{"hasIssuesEnabled":true}`)
	case strings.HasSuffix(command, "git rev-parse --abbrev-ref HEAD"):
		fmt.Fprintln(out, "main")
	case strings.HasSuffix(command, "git remote get-url --push origin"):
		if strings.Contains(command, "charlie") {
			return errors.New("exit status 2")
		}
		fmt.Fprintln(out, "git@github.com:me/bravo.git")
	case strings.HasSuffix(command, "git symbolic-ref --short refs/remotes/origin/HEAD"):
		fmt.Fprintln(out, "origin/main")
	case strings.Contains(command, "git rev-parse HEAD "):
		fmt.Fprintln(out, "0123456789abcdef0123456789abcdef01234567\n0123456789abcdef0123456789abcdef01234567")
	}
	return nil
}

func machine() *fakeShell {
	return &fakeShell{
		present:  map[string]bool{"check-git": true},
		versions: map[string]string{},
		repos:    map[string]map[string]string{},
	}
}

// withEverythingCurrent is a machine with roast and TruffleHog at their
// latest, so the tools screen has nothing to offer.
func (f *fakeShell) withEverythingCurrent() *fakeShell {
	f.present["check-roast"] = true
	f.versions["version-roast"] = "roast 1.1.0"
	f.present["command -v trufflehog"] = true
	f.versions["trufflehog --version"] = "trufflehog 3.97.4"
	return f
}

// withTrufflehogFromBrew is a machine with TruffleHog 3.97.0 linked from
// Homebrew's bin into its Cellar, both under home so the link is real,
// while the latest is 3.97.4.
func (f *fakeShell) withTrufflehogFromBrew(t *testing.T, home string) *fakeShell {
	t.Helper()
	brew := filepath.Join(home, "brew")
	target := filepath.Join(brew, "Cellar", "trufflehog", "3.97.0", "bin", "trufflehog")
	write(t, target, "#!/bin/sh\n")
	if err := os.MkdirAll(filepath.Join(brew, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(brew, "bin", "trufflehog")); err != nil {
		t.Fatal(err)
	}
	f.present["command -v trufflehog"] = true
	f.versions["command -v trufflehog"] = filepath.Join(brew, "bin", "trufflehog")
	f.versions["trufflehog --version"] = "trufflehog 3.97.0"
	f.versions["brew --prefix"] = brew
	return f
}

func latest(_ context.Context, tool tools.Tool) (string, error) {
	if tool.Title == "TruffleHog" {
		return "v3.97.4", nil
	}
	if tool.Title == "roast" {
		return "v1.1.0", nil
	}
	return "", errors.New("no network")
}

func options(t *testing.T, home string, shell *fakeShell) (setup.Options, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return setup.Options{
		Files:  fixture(),
		Home:   home,
		Getenv: func(string) string { return "" },
		Stdin:  strings.NewReader(""),
		Stdout: &out,
		Shell:  shell.run,
		Latest: latest,
		Now:    func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
	}, &out
}

func homeWithClaude(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// settled is a home a first run already asked everything in: the repo and
// the repos folder questions answered, Claude Code picked and up to date.
func settled(t *testing.T, home string, shell *fakeShell) {
	t.Helper()
	opts, _ := options(t, home, shell)
	opts.Yes = true
	opts.NoSkillRepo = true
	opts.Harness = "claude"
	if err := setup.Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, ".squirrel", "config.json"), `{"harnesses":["claude"],"harnesses_found":["claude"],"skill_repos_asked":true,"repos_dirs_asked":true}`)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

var (
	enter = tea.KeyMsg{Type: tea.KeyEnter}
	esc   = tea.KeyMsg{Type: tea.KeyEsc}
	down  = tea.KeyMsg{Type: tea.KeyDown}
	space = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	ctrlC = tea.KeyMsg{Type: tea.KeyCtrlC}
	ctrlU = tea.KeyMsg{Type: tea.KeyCtrlU}
	ctrlA = tea.KeyMsg{Type: tea.KeyCtrlA}
)

func typed(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

// press is what a test does on one screen: wait for the title, then the
// keys, the last of which ends the screen. An await among the keys waits
// for text to show before the next key, the way a person waits for the
// second page of a screen before typing on it.
type press struct {
	title string
	keys  []tea.Msg
}

type await string

func on(title string, keys ...tea.Msg) press {
	return press{title: title, keys: keys}
}

// scripted is a runner that presses the scripted keys on each screen in
// turn and keeps the frame shown before every key, so a test can read the
// screen the way a person saw it. The line a screen leaves behind goes to
// out, where the terminal would show it.
type scripted struct {
	t       *testing.T
	out     io.Writer
	size    tea.WindowSizeMsg
	presses []press
	index   int
	frames  []string
}

func (s *scripted) run(screen *screen) error {
	s.t.Helper()
	if s.index >= len(s.presses) {
		s.t.Fatalf("screen %d was not scripted:\n%s", s.index+1, ansi.Strip(screen.View()))
	}
	current := s.presses[s.index]
	s.index++
	if os.Getenv("TUI_DEBUG") != "" {
		s.t.Logf("screen %d: waiting for %q", s.index, current.title)
	}
	model := teatest.NewTestModel(s.t, &recorder{screen: screen, frames: &s.frames}, teatest.WithInitialTermSize(s.size.Width, s.size.Height))
	// teatest sends no size for a zero one; a pty nothing sized reports
	// zero by zero, and bubbletea passes that on.
	if s.size.Width == 0 {
		model.Send(s.size)
	}
	teatest.WaitFor(s.t, model.Output(), func(shown []byte) bool {
		return bytes.Contains(shown, []byte(current.title))
	}, teatest.WithDuration(5*time.Second))
	for _, key := range current.keys {
		if text, ok := key.(await); ok {
			teatest.WaitFor(s.t, model.Output(), func(shown []byte) bool {
				return bytes.Contains(shown, []byte(text))
			}, teatest.WithDuration(5*time.Second))
			continue
		}
		model.Send(key)
	}
	model.WaitFinished(s.t, teatest.WithFinalTimeout(5*time.Second))
	if os.Getenv("TUI_DEBUG") != "" {
		s.t.Logf("screen %d ended with outcome %d, summary %q", s.index, screen.outcome, ansi.Strip(screen.View()))
	}
	fmt.Fprint(s.out, ansi.Strip(screen.View()))
	return nil
}

// recorder keeps the frame shown before each key.
type recorder struct {
	screen *screen
	frames *[]string
}

func (r *recorder) Init() tea.Cmd { return r.screen.Init() }

func (r *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		*r.frames = append(*r.frames, ansi.Strip(r.screen.View()))
	}
	_, cmd := r.screen.Update(msg)
	return r, cmd
}

func (r *recorder) View() string { return r.screen.View() }

// terminalOf is the terminal a test runs the flow on: its size, and how
// long a wait runs before its line shows, an hour unless the test is
// about the line.
type terminalOf struct {
	size  tea.WindowSizeMsg
	after time.Duration
}

var wide = terminalOf{size: tea.WindowSizeMsg{Width: 100, Height: 40}, after: time.Hour}

func guided(t *testing.T, opts setup.Options, presses ...press) (*scripted, error) {
	t.Helper()
	return wide.guided(t, opts, presses...)
}

// guided runs the flow the way Run does, the wait line wired in and the
// screens scripted.
func (term terminalOf) guided(t *testing.T, opts setup.Options, presses ...press) (*scripted, error) {
	t.Helper()
	out := opts.Stdout
	waiting := newWait(out, term.after)
	opts.Progress, opts.Stdout = waiting.report, waiting
	session, err := setup.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	script := &scripted{t: t, out: opts.Stdout, size: term.size, presses: presses}
	err = run(context.Background(), session, opts, func(s *screen) error {
		waiting.clear()
		return script.run(s)
	})
	if script.index != len(presses) {
		t.Fatalf("%d screens scripted, %d shown; output:\n%s", len(presses), script.index, out.(*bytes.Buffer).String())
	}
	return script, err
}

// frame is the last frame that has the text, or a failure naming them all.
func (s *scripted) frame(text string) string {
	s.t.Helper()
	for index := len(s.frames) - 1; index >= 0; index-- {
		if strings.Contains(s.frames[index], text) {
			return s.frames[index]
		}
	}
	s.t.Fatalf("no frame has %q; frames:\n%s", text, strings.Join(s.frames, "\n----\n"))
	return ""
}

func expectAll(t *testing.T, output string, expected ...string) {
	t.Helper()
	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRerunWithNothingChangedIsOneEnter(t *testing.T) {
	home := homeWithClaude(t)
	shell := machine().withEverythingCurrent()
	settled(t, home, shell)
	opts, out := options(t, home, shell)
	script, err := guided(t, opts, on("Install into which harnesses?", enter))
	if err != nil {
		t.Fatal(err)
	}
	harnesses := script.frame("Install into which harnesses?")
	if !strings.Contains(harnesses, "✓ Claude Code") || !strings.Contains(harnesses, "• Codex") || !strings.Contains(harnesses, "esc back") {
		t.Fatalf("harness screen:\n%s", harnesses)
	}
	expectAll(t, out.String(), "skills   up to date in ~/.claude/skills", "Everything is in place. Nothing to apply.", "harness picks saved to ~/.squirrel/config.json")
}

func TestHarnessFoundSinceTheLastRunIsOfferedChecked(t *testing.T) {
	home := homeWithClaude(t)
	shell := machine().withEverythingCurrent()
	settled(t, home, shell)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts, out := options(t, home, shell)
	script, err := guided(t, opts, on("Install into which harnesses?", enter), on("Apply?", enter))
	if err != nil {
		t.Fatal(err)
	}
	harnesses := script.frame("Install into which harnesses?")
	if !strings.Contains(harnesses, "✓ Claude Code") || !strings.Contains(harnesses, "✓ Codex") {
		t.Fatalf("harness screen:\n%s", harnesses)
	}
	expectAll(t, out.String(), "harnesses  Claude Code, Codex", "Codex  ~/.codex/skills\n  new      voice", "apply  yes", "skills   1 installed, 0 updated in ~/.codex/skills")
	if !strings.Contains(read(t, filepath.Join(home, ".squirrel", "config.json")), `"codex"`) {
		t.Fatal("the new harness was not saved")
	}
}

func TestHarnessUncheckedOnPurposeStaysUnchecked(t *testing.T) {
	home := homeWithClaude(t)
	shell := machine().withEverythingCurrent()
	settled(t, home, shell)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts, out := options(t, home, shell)
	if _, err := guided(t, opts, on("Install into which harnesses?", down, space, enter)); err != nil {
		t.Fatal(err)
	}
	expectAll(t, out.String(), "harnesses  Claude Code\n", "Everything is in place. Nothing to apply.")
	opts, _ = options(t, home, shell)
	script, err := guided(t, opts, on("Install into which harnesses?", enter))
	if err != nil {
		t.Fatal(err)
	}
	harnesses := script.frame("Install into which harnesses?")
	if !strings.Contains(harnesses, "✓ Claude Code") || !strings.Contains(harnesses, "• Codex") {
		t.Fatalf("Codex came back checked after being left out:\n%s", harnesses)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills")); err == nil {
		t.Fatal("skills were written into the harness left out")
	}
}

func TestEscGoesBackAScreenAndTheAnswerStays(t *testing.T) {
	home := homeWithClaude(t)
	shell := machine()
	opts, out := options(t, home, shell)
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", esc),
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Which tools?", esc),
		on("Where do your repos live?", enter),
		on("Which tools?", enter),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.Join(script.frames, "\n"), "Install into which harnesses?") != 2 {
		t.Fatalf("Esc did not show the harness screen again:\n%s", strings.Join(script.frames, "\n----\n"))
	}
	expectAll(t, out.String(), "harnesses  Claude Code", "skills repo  none", "repos  none", "tools  none", "apply  yes", "skills   1 installed, 0 updated in ~/.claude/skills")
	if !strings.Contains(read(t, filepath.Join(home, ".squirrel", "config.json")), `"skill_repos_asked": true`) {
		t.Fatal("the skipped repo question was not remembered")
	}
}

func TestEscOnTheFirstScreenAndCtrlCQuitWithNothingChanged(t *testing.T) {
	for name, key := range map[string]tea.Msg{"esc": esc, "ctrl+c": ctrlC} {
		home := homeWithClaude(t)
		opts, _ := options(t, home, machine())
		_, err := guided(t, opts, on("Install into which harnesses?", key))
		if !errors.Is(err, ErrQuit) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); err == nil {
			t.Fatalf("%s: skills were installed", name)
		}
	}
}

func TestSkillRepoIsValidatedAndItsCollisionAsked(t *testing.T) {
	home := homeWithClaude(t)
	shell := machine()
	shell.repos["me/work-skills"] = map[string]string{"skills/voice/SKILL.md": "voice, my way\n"}
	opts, out := options(t, home, shell)
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", typed("not a repo"), enter, ctrlU, typed("me/work-skills"), enter),
		on(`Skill "voice" is in squirrel and me/work-skills`, down, enter),
		on("Where do your repos live?", enter),
		on("Which tools?", enter),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script.frame("not owner/name"), `"not a repo" is not owner/name`) {
		t.Fatal("the bad name was not refused on the screen")
	}
	collision := script.frame("Which one goes into the harnesses?")
	if !strings.Contains(collision, "keep squirrel's") || !strings.Contains(collision, "use me/work-skills's") || !strings.Contains(collision, "rename it yourself") {
		t.Fatalf("collision screen:\n%s", collision)
	}
	expectAll(t, out.String(), "skills repo  me/work-skills", "me/work-skills  ~/.squirrel/repos/me/work-skills, cloned, 1 skills", "voice  use me/work-skills's", "voice  overridden by me/work-skills, not installed from squirrel")
	if got := read(t, filepath.Join(home, ".claude", "skills", "voice", "SKILL.md")); got != "voice, my way\n" {
		t.Fatalf("voice = %q", got)
	}
}

func TestRenameChoiceStopsBeforeAnyHarnessChanges(t *testing.T) {
	home := homeWithClaude(t)
	shell := machine()
	shell.repos["me/work-skills"] = map[string]string{"skills/voice/SKILL.md": "voice, my way\n"}
	opts, _ := options(t, home, shell)
	_, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", typed("me/work-skills"), enter),
		on(`Skill "voice" is in squirrel and me/work-skills`, down, down, enter),
	)
	if err == nil || !strings.Contains(err.Error(), `rename the "voice" folder in me/work-skills`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); err == nil {
		t.Fatal("skills were installed")
	}
}

// homeWithRepos is a home with ~/code holding alpha, which declares its
// tracker, bravo, which doesn't and can take a PR, and charlie, which
// doesn't and has no origin.
func homeWithRepos(t *testing.T) string {
	t.Helper()
	home := homeWithClaude(t)
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if err := os.MkdirAll(filepath.Join(home, "code", name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(home, "code", "alpha", "AGENTS.md"), "# Alpha\n\nTracker: markdown tasks/\n")
	write(t, filepath.Join(home, "code", "bravo", "AGENTS.md"), "# Bravo\n\nSome text.\n")
	return home
}

// homeWithTeams is a home with ~/code holding eight undeclared repos named
// for the Linear teams a person would put them in: five for SR, three for
// KC. Nothing on disk says so; the names are for the tests to filter by.
func homeWithTeams(t *testing.T) string {
	t.Helper()
	home := homeWithClaude(t)
	for _, name := range []string{"kc-api", "kc-app", "kc-site", "sr-api", "sr-cli", "sr-docs", "sr-site", "sr-web"} {
		if err := os.MkdirAll(filepath.Join(home, "code", name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestBackendScreensDeclareTheReposAndOnePRQuestionCoversThem(t *testing.T) {
	home := homeWithRepos(t)
	shell := machine()
	opts, out := options(t, home, shell)
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", typed("SR"), enter),
		on("Which repos track their work in Linear team SR?", space, enter),
		on("Another Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which repos track their work in markdown tasks in the repo?", space, enter, await("markdown tasks in the repo folder"), enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("y")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	repos := script.frame("Where do your repos live?")
	if !strings.Contains(repos, "~/code") || !strings.Contains(repos, "somewhere else") {
		t.Fatalf("repos screen:\n%s", repos)
	}
	if key := script.frame("Linear team key, Enter for none"); !strings.Contains(key, "for example SR") {
		t.Fatalf("key screen:\n%s", key)
	}
	linear := script.frame("Which repos track their work in Linear team SR?")
	for _, want := range []string{"✓ bravo", "• charlie", "Unchecked on every screen means skip."} {
		if !strings.Contains(linear, want) {
			t.Fatalf("Linear screen missing %q:\n%s", want, linear)
		}
	}
	if issues := script.frame("Which repos track their work in GitHub Issues?"); strings.Contains(issues, "bravo") || !strings.Contains(issues, "• charlie") {
		t.Fatalf("a repo taken by Linear was offered again:\n%s", issues)
	}
	if markdown := script.frame("markdown tasks in the repo folder"); !strings.Contains(markdown, "Enter for tasks/") {
		t.Fatalf("folder page:\n%s", markdown)
	}
	expectAll(t, out.String(),
		"repos  ~/code",
		"linear SR  1 repo\n",
		"github-issues  none\n",
		"markdown tasks/  1 repo\n",
		"\ntrackers\n  bravo    write \"Tracker: linear SR\", open a PR on branch tracker-line\n  charlie  write \"Tracker: markdown tasks/\" only, has no origin remote\n",
		"pull requests  yes\n",
		`bravo  wrote "Tracker: linear SR" to ~/code/bravo/AGENTS.md`,
		"bravo  PR opened, back on main",
		`charlie  wrote "Tracker: markdown tasks/" to ~/code/charlie/AGENTS.md`,
		"charlie  has no origin remote; the line is left uncommitted",
	)
	if !strings.Contains(strings.Join(shell.commands, "\n"), "gh pr create") {
		t.Fatalf("no PR was opened:\n%s", strings.Join(shell.commands, "\n"))
	}
	if !strings.Contains(read(t, filepath.Join(home, ".squirrel", "config.json")), `"repos_dirs_asked": true`) {
		t.Fatal("the repos folder was not remembered")
	}
}

func TestEachKeyGetsItsOwnListAndEveryRepoItsOwnLine(t *testing.T) {
	home := homeWithTeams(t)
	opts, out := options(t, home, machine())
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", typed("SR"), enter),
		on("Which repos track their work in Linear team SR?", typed("/"), typed("sr-"), enter, ctrlA, esc, enter),
		on("Another Linear team key, Enter for none", typed("KC"), enter),
		on("Which repos track their work in Linear team KC?", typed("/"), typed("kc-"), enter, ctrlA, esc, enter),
		on("Which tools?", enter),
		on("Open 8 pull requests?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	if kc := script.frame("Which repos track their work in Linear team KC?"); strings.Contains(kc, "sr-") || !strings.Contains(kc, "✓ kc-app") {
		t.Fatalf("the KC list offers what SR took, or lost its checks:\n%s", kc)
	}
	expectAll(t, out.String(),
		"linear SR  5 repos, linear KC  3 repos\n",
		"pull requests  no, lines left uncommitted\n",
	)
	for repo, line := range map[string]string{
		"sr-api": "Tracker: linear SR", "sr-cli": "Tracker: linear SR", "sr-docs": "Tracker: linear SR", "sr-site": "Tracker: linear SR", "sr-web": "Tracker: linear SR",
		"kc-api": "Tracker: linear KC", "kc-app": "Tracker: linear KC", "kc-site": "Tracker: linear KC",
	} {
		if got := read(t, filepath.Join(home, "code", repo, "AGENTS.md")); !strings.Contains(got, line+"\n") {
			t.Fatalf("%s got %q, want %q", repo, got, line)
		}
	}
}

func TestTheSameKeyTwiceIsOneLine(t *testing.T) {
	home := homeWithRepos(t)
	opts, out := options(t, home, machine())
	_, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", typed("SR"), enter),
		on("Which repos track their work in Linear team SR?", space, enter),
		on("Another Linear team key, Enter for none", typed("SR"), enter),
		on("Which repos track their work in Linear team SR?", space, enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	expectAll(t, out.String(), "linear SR  2 repos\n", `bravo  wrote "Tracker: linear SR"`, `charlie  wrote "Tracker: linear SR"`)
}

func TestOriginWithIssuesComesCheckedAndSkipsAreRememberedUntilAskedForAgain(t *testing.T) {
	home := homeWithRepos(t)
	shell := machine()
	opts, out := options(t, home, shell)
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which repos track their work in markdown tasks in the repo?", enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	issues := script.frame("Which repos track their work in GitHub Issues?")
	for _, want := range []string{"✓ bravo", "• charlie", "1 repo checked already, from the origin."} {
		if !strings.Contains(issues, want) {
			t.Fatalf("GitHub Issues screen missing %q:\n%s", want, issues)
		}
	}
	expectAll(t, out.String(),
		"linear  none\n",
		"github-issues  1 repo\n",
		"markdown  none\n",
		"\ntrackers\n  bravo    write \"Tracker: github-issues\", open a PR on branch tracker-line\n  charlie  skipped; not offered again without --ask-trackers-again\n",
		"pull requests  no, lines left uncommitted\n",
		`bravo  wrote "Tracker: github-issues"`,
		"bravo  line left uncommitted; commit it yourself",
		"charlie  skipped",
	)
	if strings.Contains(strings.Join(shell.commands, "\n"), "checkout -b") {
		t.Fatal("a PR was opened after No")
	}
	if got := read(t, filepath.Join(home, ".squirrel", "config.json")); !strings.Contains(got, `"trackers_skipped": [`) || !strings.Contains(got, "charlie") {
		t.Fatalf("config = %s", got)
	}

	opts, out = options(t, home, shell)
	_, err = guided(t, opts, on("Install into which harnesses?", enter), on("Which tools?", enter))
	if err != nil {
		t.Fatal(err)
	}
	expectAll(t, out.String(), "  bravo    Tracker: github-issues\n  charlie  not declared, skipped on an earlier run; add --ask-trackers-again to be asked\n", "Everything is in place. Nothing to apply.")

	opts, out = options(t, home, shell)
	opts.AskTrackersAgain = true
	_, err = guided(t, opts,
		on("Install into which harnesses?", enter),
		on("Linear team key, Enter for none", typed("SR"), enter),
		on("Which repos track their work in Linear team SR?", space, enter),
		on("Which tools?", enter),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	expectAll(t, out.String(), "linear SR  1 repo\n", `charlie  wrote "Tracker: linear SR"`)
	if got := read(t, filepath.Join(home, ".squirrel", "config.json")); strings.Contains(got, "trackers_skipped") {
		t.Fatalf("a declared repo stayed on the skip list: %s", got)
	}
}

func TestSlashFiltersTheListAndEscClearsTheFilterBeforeGoingBack(t *testing.T) {
	home := homeWithRepos(t)
	opts, out := options(t, home, machine())
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", typed("SR"), enter),
		on("Which repos track their work in Linear team SR?", typed("/"), typed("cha"), enter, space, esc, enter),
		on("Another Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("y")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	var filtered, cleared string
	for _, frame := range script.frames {
		if strings.Contains(frame, "Which repos track their work in Linear team SR?") && strings.Contains(frame, "cha") {
			if strings.Contains(frame, "✓ charlie") {
				cleared = frame
			} else if strings.Contains(frame, "• charlie") {
				filtered = frame
			}
		}
	}
	if filtered == "" || strings.Contains(filtered, "bravo") {
		t.Fatalf("the filter did not narrow the list to charlie:\n%s", filtered)
	}
	if cleared == "" || !strings.Contains(cleared, "• bravo") {
		t.Fatalf("Esc did not clear the filter and bring bravo back:\n%s", cleared)
	}
	expectAll(t, out.String(), "linear SR  1 repo\n", "github-issues  1 repo\n", `charlie  wrote "Tracker: linear SR"`, `bravo  wrote "Tracker: github-issues"`)
}

func TestEscWalksTheRoundsBackWithTheirKeysAndChecksKept(t *testing.T) {
	home := homeWithRepos(t)
	opts, out := options(t, home, machine())
	script, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", typed("KC"), enter),
		on("Which repos track their work in Linear team KC?", space, enter),
		on("Another Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", esc),
		on("Another Linear team key, Enter for none", esc),
		on("Which repos track their work in Linear team KC?", esc),
		on("Linear team key, Enter for none", esc),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", enter),
		on("Which repos track their work in Linear team KC?", enter),
		on("Another Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which repos track their work in markdown tasks in the repo?", enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for _, frame := range script.frames {
		if strings.Contains(frame, "Linear team key, Enter for none") && !strings.Contains(frame, "Another") {
			key = frame
		}
	}
	if !strings.Contains(key, "KC") {
		t.Fatalf("the key was lost on the way back:\n%s", key)
	}
	if again := script.frame("Which repos track their work in Linear team KC?"); !strings.Contains(again, "✓ bravo") {
		t.Fatalf("bravo lost its check after Esc:\n%s", again)
	}
	expectAll(t, out.String(), "linear KC  1 repo\n", `bravo  wrote "Tracker: linear KC"`, "charlie  skipped")
}

func TestToolsScreenOffersTheOwnersUpdateLineAndUncheckedMeansSkip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew is not where Windows keeps tools")
	}
	home := homeWithClaude(t)
	shell := machine().withTrufflehogFromBrew(t, home)
	settled(t, home, shell)
	opts, out := options(t, home, shell)
	script, err := guided(t, opts, on("Install into which harnesses?", enter), on("Which tools?", enter))
	if err != nil {
		t.Fatal(err)
	}
	tools := script.frame("Which tools?")
	for _, want := range []string{"install roast: curl roast | sh", "update TruffleHog 3.97.0 to 3.97.4: brew upgrade trufflehog", "Unchecked is skipped.", "ok git"} {
		if !strings.Contains(tools, want) {
			t.Fatalf("tools screen missing %q:\n%s", want, tools)
		}
	}
	expectAll(t, out.String(), "tools  none", "outdated TruffleHog 3.97.0, latest 3.97.4. update: brew upgrade trufflehog")
	if strings.Contains(strings.Join(shell.commands, "\n"), "brew upgrade") {
		t.Fatal("an unchecked tool was updated")
	}
	shell.commands = nil
	opts, out = options(t, home, shell)
	_, err = guided(t, opts, on("Install into which harnesses?", enter), on("Which tools?", down, space, enter), on("Apply?", enter))
	if err != nil {
		t.Fatal(err)
	}
	expectAll(t, out.String(), "tools  update TruffleHog 3.97.0 to 3.97.4", "updating TruffleHog 3.97.0 to 3.97.4: brew upgrade trufflehog", "updated TruffleHog 3.97.4")
	if !strings.Contains(strings.Join(shell.commands, "\n"), "brew upgrade trufflehog") {
		t.Fatalf("brew upgrade did not run:\n%s", strings.Join(shell.commands, "\n"))
	}
}

func TestNoOnThePlanChangesNothing(t *testing.T) {
	home := homeWithClaude(t)
	opts, out := options(t, home, machine())
	_, err := guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Which tools?", enter),
		on("Apply?", typed("n")),
	)
	if err != nil {
		t.Fatal(err)
	}
	expectAll(t, out.String(), "Claude Code  ~/.claude/skills\n  new      voice", "apply  no", "Nothing changed in the harnesses.")
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); err == nil {
		t.Fatal("skills were installed after No")
	}
}

func TestSequenceWalksBackOverSkippedStepsIntoTheLastOfAList(t *testing.T) {
	var trail []string
	shown := func(name string, results ...outcome) step {
		calls := 0
		return func(direction) (outcome, error) {
			trail = append(trail, name)
			result := results[min(calls, len(results)-1)]
			calls++
			return result, nil
		}
	}
	hidden := func(direction) (outcome, error) { return skipped, nil }
	result, err := sequence(
		shown("a", next),
		hidden,
		sequence(shown("b", next), shown("c", next)),
		hidden,
		shown("d", back, next),
	)(forward)
	if err != nil || result != next {
		t.Fatalf("result = %v, err = %v", result, err)
	}
	if got := strings.Join(trail, " "); got != "a b c d c d" {
		t.Fatalf("trail = %q, want Esc on d to show c, the last of the list, then d again", got)
	}
	result, err = sequence(hidden, shown("a", back))(forward)
	if err != nil || result != back {
		t.Fatalf("Esc off the front: result = %v, err = %v", result, err)
	}
	if result, _ = sequence(hidden, hidden)(forward); result != skipped {
		t.Fatalf("a list with nothing shown = %v, want skipped", result)
	}
}

// countsOf is every count a step drew on the line, in the order drawn.
func countsOf(output, doing string) []int {
	var counts []int
	for _, draw := range strings.Split(output, clearLine) {
		if _, rest, ok := strings.Cut(draw, doing+" "); ok {
			var count int
			fmt.Sscanf(rest, "%d of", &count)
			counts = append(counts, count)
		}
	}
	return counts
}

func TestTheScanCountsUpOnOneLineUntilTheFirstListReplacesIt(t *testing.T) {
	home := homeWithTeams(t)
	shell := machine()
	shell.delay = 10 * time.Millisecond
	opts, out := options(t, home, shell)
	slow := terminalOf{size: wide.size, after: 20 * time.Millisecond}
	script, err := slow.guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which tools?", enter),
		on("Open 8 pull requests?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	shown := out.String()
	counts := countsOf(shown, "reading repos")
	if len(counts) == 0 || !slices.IsSorted(counts) || counts[len(counts)-1] != 8 {
		t.Fatalf("the repos count did not climb to 8 in place: %v in\n%q", counts, shown)
	}
	expectAll(t, shown,
		clearLine+"reading repos 8 of 8, asking gh about origins"+clearLine+"linear  none\ngithub-issues  8 repos\n",
		clearLine+"checking tools 3 of 3, looking up latest versions 2 of 2"+clearLine+"tools  ",
	)
	if list := script.frame("Which repos track their work in GitHub Issues?"); strings.Contains(list, "reading repos") {
		t.Fatalf("the line is still up under the list:\n%s", list)
	}
}

func TestAScanThatEndsInsideASecondShowsNoLine(t *testing.T) {
	home := homeWithRepos(t)
	opts, out := options(t, home, machine())
	quick := terminalOf{size: wide.size, after: time.Second}
	_, err := quick.guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which repos track their work in markdown tasks in the repo?", enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	if shown := out.String(); strings.Contains(shown, "\r") || strings.Contains(shown, "reading repos") {
		t.Fatalf("a line showed for a three-repo scan:\n%q", shown)
	}
}

func TestATerminalThatReportsNoSizeStillShowsTheLists(t *testing.T) {
	home := homeWithRepos(t)
	opts, out := options(t, home, machine())
	unsized := terminalOf{size: tea.WindowSizeMsg{}, after: time.Hour}
	script, err := unsized.guided(t, opts,
		on("Install into which harnesses?", enter),
		on("skills repo of your own", enter),
		on("Where do your repos live?", enter),
		on("Linear team key, Enter for none", enter),
		on("Which repos track their work in GitHub Issues?", enter),
		on("Which repos track their work in markdown tasks in the repo?", enter),
		on("Which tools?", enter),
		on("Open 1 pull request?", typed("n")),
		on("Apply?", enter),
	)
	if err != nil {
		t.Fatal(err)
	}
	if list := script.frame("Which repos track their work in GitHub Issues?"); !strings.Contains(list, "✓ bravo") || !strings.Contains(list, "• charlie") {
		t.Fatalf("the list did not draw at no size:\n%s", list)
	}
	expectAll(t, out.String(), "github-issues  1 repo\n", `bravo  wrote "Tracker: github-issues"`)
}
