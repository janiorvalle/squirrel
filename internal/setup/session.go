package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/janiorvalle/squirrel/internal/harness"
	"github.com/janiorvalle/squirrel/internal/letter"
	"github.com/janiorvalle/squirrel/internal/skills"
	"github.com/janiorvalle/squirrel/internal/tools"
)

// Session is one run of setup: the embedded assets, the saved config, and
// the harness table, read once, plus the facts a guided flow asks for as
// its screens come up. Each fact is computed the first time it's asked for
// and kept, so a screen shown again after Esc costs nothing. Run and the
// guided flow both go through it, so every rule lives here once.
type Session struct {
	opts     Options
	embedded assets
	config   Config
	rows     harness.Table
	found    []string

	gathered    catalog
	gatheredFor string
	gatheredYet bool
	checked     []toolStatus
	checkedYet  bool
	scanned     []TrackerQuestion
	scannedFor  string
	scannedYet  bool
	origins     map[string]originFacts
	metOrigins  []string
}

// Start reads what every screen needs. Nothing on the machine changes.
func Start(opts Options) (*Session, error) {
	embedded, err := loadAssets(opts.Files)
	if err != nil {
		return nil, err
	}
	config, err := loadConfig(opts.Home)
	if err != nil {
		return nil, err
	}
	rows := harness.Resolve(opts.Home, opts.Getenv)
	return &Session{opts: opts, embedded: embedded, config: config, rows: rows, found: harness.Keys(rows.Found()), origins: rememberedOrigins(config, opts)}, nil
}

// rememberedOrigins is what earlier runs learned from gh about origins,
// every one of them seen, or nothing under --ask-trackers-again, the flag
// that asks everything again.
func rememberedOrigins(config Config, opts Options) map[string]originFacts {
	origins := map[string]originFacts{}
	if opts.AskTrackersAgain {
		return origins
	}
	for url, facts := range config.Origins {
		origins[url] = originFacts{seen: true, OriginFacts: facts}
	}
	return origins
}

// HarnessChoice is one row of the harness screen. Checked is the pick to
// start from: --harness when given, else the saved picks plus every
// harness found that the screen never offered before, so one installed
// since the last run is offered while one unchecked on purpose stays so.
type HarnessChoice struct {
	Key     string
	Name    string
	Where   string
	Found   bool
	Checked bool
}

// Harnesses is the harness screen's rows, in table order.
func (s *Session) Harnesses() ([]HarnessChoice, error) {
	picked, source, err := choose(s.opts, s.rows, s.config)
	if err != nil {
		return nil, err
	}
	// A config from before the found set was recorded says nothing about
	// what was offered, so everything found now counts as offered then: a
	// harness the person left out stays out, and the set is saved from
	// this run on.
	offeredKeys := s.config.HarnessesFound
	if len(s.config.Harnesses) > 0 && offeredKeys == nil {
		offeredKeys = s.found
	}
	offered := map[string]bool{}
	for _, key := range offeredKeys {
		offered[key] = true
	}
	var choices []HarnessChoice
	for _, entry := range s.rows {
		checked := contains(picked, entry) || source == fromConfig && entry.Installed() && !offered[entry.Key]
		choices = append(choices, HarnessChoice{Key: entry.Key, Name: entry.Name, Where: rootWithVariable(s.opts.Home, entry), Found: entry.Installed(), Checked: checked})
	}
	return choices, nil
}

// SkillRepos is the skills repos this run installs from before any
// question: the saved ones and the ones the flags name.
func (s *Session) SkillRepos() ([]string, error) {
	return chooseRepos(s.config.SkillRepos, s.opts)
}

// SkillRepoAsked reports whether the skills repo question is settled: it
// was asked on an earlier run, --no-skill-repo said there is none, or a
// repo is already named.
func (s *Session) SkillRepoAsked() bool {
	names, err := s.SkillRepos()
	return s.config.SkillReposAsked || s.opts.NoSkillRepo || (err == nil && len(names) > 0)
}

// RepoName normalizes what a person types or pastes for a skills repo to
// owner/name, or says what shape was expected.
func RepoName(spec string) (string, error) {
	return repoName(spec)
}

// Gather fetches the repos, prints them, and settles every skill name more
// than one source holds with the --override flags and the saved picks. It
// returns the collisions those don't settle, for the guided flow to ask
// about, one screen each. Gathering happens once per set of names.
func (s *Session) Gather(ctx context.Context, repoNames []string) ([]skills.Collision, error) {
	key := strings.Join(repoNames, ",")
	if s.gatheredYet && s.gatheredFor == key {
		return s.gathered.open, nil
	}
	if s.gatheredYet {
		s.gathered.close()
	}
	s.gatheredYet, s.gatheredFor = true, key
	s.gathered = holdBack(buildCatalog(s.embedded, syncRepos(ctx, s.opts, repoNames)), s.config.SkillOverrides)
	printRepos(s.opts.Stdout, s.opts.Home, s.gathered.repos)
	printHeld(s.opts.Stdout, s.gathered, s.config.SkillOverrides)
	collisions, err := skills.Collisions(s.gathered.sources)
	if err != nil {
		return nil, err
	}
	s.gathered.collisions = collisions
	picks, open, err := settleCollisions(collisions, s.config.SkillOverrides, s.opts.Overrides)
	if err != nil {
		return nil, err
	}
	s.gathered.picks, s.gathered.open = picks, open
	return open, nil
}

// Rename is the pick that stops setup so the person can rename the folder
// in their repo instead of choosing.
const Rename = "rename"

// PickSources takes the answers for the open collisions, one source per
// skill name, and prints where every colliding name comes from this run.
func (s *Session) PickSources(picks map[string]string) error {
	for _, collision := range s.gathered.open {
		source := picks[collision.Name]
		if source == Rename {
			return renameStop(collision)
		}
		if !holds(collision, source) {
			return fmt.Errorf("[SQUIRREL-OVERRIDE] skill %q is not in %s; it is in %s", collision.Name, source, strings.Join(collision.Sources, " and "))
		}
		s.gathered.picks[collision.Name] = source
	}
	printOverrides(s.opts.Stdout, s.gathered.collisions, s.gathered.picks)
	return nil
}

// Display is a path the way the report shows it, ~ for home.
func Display(home, path string) string {
	return display(home, path)
}

// ReposDirs is the folders this run scans before any question: the saved
// ones and the ones --repos-dir names.
func (s *Session) ReposDirs() ([]string, error) {
	dirs, _, err := chooseReposDirs(s.config, s.opts)
	return dirs, err
}

// ReposDirsAsked reports whether the repos folder question is settled.
func (s *Session) ReposDirsAsked() bool {
	_, asked, err := chooseReposDirs(s.config, s.opts)
	return err == nil && asked
}

// ReposDirGuesses is every folder under home that looks like a repos
// folder, for the screen to offer.
func (s *Session) ReposDirGuesses() []string {
	return guessReposDirs(s.opts.Home)
}

// ParseReposDirs turns what a person typed into folders that exist, comma
// separated for more than one, ~ for home.
func ParseReposDirs(answer, home string) ([]string, error) {
	return parseReposDirs(answer, home)
}

// TrackerQuestion is one repo the tracker screens are for this run. Dir
// is the checkout, what an answer names, since two repos folders can each
// hold a repo called app. Repo is its name, for the screen. File is where
// the line would be written, shown the way the report shows paths. Guess
// is the backend key the origin suggests, github-issues for a repo gh can
// see with issues enabled, "" when the origin says nothing. PRHold is why
// the PR can't be opened, in words, "" when it can: a clean tree on its
// default branch at the remote's commit, with an origin gh can see, read
// before any write so the line itself never counts as pending.
type TrackerQuestion struct {
	Dir    string
	Repo   string
	File   string
	Guess  string
	PRHold string
}

// Trackers scans the folders and reads each undeclared repo's state, once
// per set of folders: git for every repo, then gh for every origin no run
// has asked about, a bounded number at a time. The questions come back in
// the repos' order, by name. A repo skipped on an earlier run is left out
// unless --ask-trackers-again was passed.
func (s *Session) Trackers(ctx context.Context, reposDirs []string) []TrackerQuestion {
	key := strings.Join(reposDirs, ",")
	if s.scannedYet && s.scannedFor == key {
		return s.scanned
	}
	s.scannedYet, s.scannedFor = true, key
	repos := planRepos(s.opts.Home, reposDirs, s.skippedEarlier()).undeclared()
	states := readRepoStates(ctx, s.opts, repos)
	s.askOrigins(ctx, states)
	s.scanned = nil
	for index, repo := range repos {
		origin := s.origins[states[index].origin]
		s.scanned = append(s.scanned, TrackerQuestion{Dir: repo.dir, Repo: repo.name, File: display(s.opts.Home, repo.file), Guess: origin.guess(), PRHold: prHold(states[index], origin)})
	}
	return s.scanned
}

// skippedEarlier is the checkouts the tracker screens leave out this run:
// the ones skipped on an earlier run, or none with --ask-trackers-again.
func (s *Session) skippedEarlier() map[string]bool {
	skipped := map[string]bool{}
	if s.opts.AskTrackersAgain {
		return skipped
	}
	for _, dir := range s.config.TrackersSkipped {
		skipped[dir] = true
	}
	return skipped
}

// askOrigins asks gh about every origin the states name that no earlier
// run or scan asked about, each once, so two checkouts of one repo cost
// one call, Esc costs nothing, and a rerun costs only the new origins. It
// also notes which origins this scan met, for what the config keeps.
func (s *Session) askOrigins(ctx context.Context, states []repoState) {
	s.metOrigins = nil
	var unknown []string
	met := map[string]bool{}
	for _, state := range states {
		url := state.origin
		if url == "" || met[url] {
			continue
		}
		met[url] = true
		s.metOrigins = append(s.metOrigins, url)
		if _, known := s.origins[url]; !known {
			unknown = append(unknown, url)
		}
	}
	for index, facts := range readOrigins(ctx, s.opts, unknown) {
		s.origins[unknown[index]] = facts
	}
}

// rememberOrigins is what the next run knows about origins: the ones this
// run's scan met and gh could see, from memory or from gh, so an origin
// whose repo has named its tracker or is gone drops off, and one gh
// couldn't see is asked again next run. A run that scanned nothing, the
// flag path, keeps what it started with: the map as it was, or nothing
// under --ask-trackers-again.
func (s *Session) rememberOrigins() map[string]OriginFacts {
	kept := map[string]OriginFacts{}
	met := s.metOrigins
	if !s.scannedYet {
		met = slices.Sorted(maps.Keys(s.origins))
	}
	for _, url := range met {
		if facts := s.origins[url]; facts.seen {
			kept[url] = facts.OriginFacts
		}
	}
	return kept
}

// Backend is one of the trackers the tracker skill knows. Argument names
// the one thing it needs after its key, "" when it needs nothing; Example
// is what the question shows and Default what Enter picks.
type Backend struct {
	Key      string
	Label    string
	Argument string
	Example  string
	Default  string
}

// Backends lists them in the order the question shows.
func Backends() []Backend {
	list := make([]Backend, 0, len(backends))
	for _, entry := range backends {
		list = append(list, Backend{Key: entry.key, Label: entry.label, Argument: entry.argument, Example: entry.example, Default: entry.byDefault})
	}
	return list
}

// TrackerLine is the line the tracker skill reads, "Tracker: linear SR".
func TrackerLine(chosen Backend, argument string) string {
	return trackerLine(backend{key: chosen.Key}, argument)
}

// TrackerAnswer is what to do about the undeclared repo at Dir: write
// Line, or skip it this run, and open the PR for the line when the repo
// can take one. Repo is its name, for the plan.
type TrackerAnswer struct {
	Dir    string
	Repo   string
	Line   string
	Skip   bool
	OpenPR bool
}

// ToolChoice is one tool on the tools screen. Actionable is true when
// setup has a line it could run: an install when missing, an update when
// outdated and the binary's owner has one. Label is that offer, and State
// the tool's line in the plan for the rest. Checked is what the flags
// already agreed to.
type ToolChoice struct {
	Title      string
	Label      string
	State      string
	Actionable bool
	Checked    bool
}

// Tools runs every check and version line and looks up the latest
// versions, once per run.
func (s *Session) Tools(ctx context.Context) []ToolChoice {
	statuses := s.toolStatuses(ctx)
	choices := make([]ToolChoice, 0, len(statuses))
	for _, status := range statuses {
		choice := ToolChoice{Title: status.tool.Title, State: toolState(status), Actionable: status.actionable(), Checked: agreedByFlag(s.opts, status)}
		if choice.Actionable {
			choice.Label = toolOffer(status)
		}
		choices = append(choices, choice)
	}
	return choices
}

func (s *Session) toolStatuses(ctx context.Context) []toolStatus {
	if s.checkedYet {
		return s.checked
	}
	s.checkedYet = true
	s.checked = checkTools(ctx, s.opts, s.embedded.tools)
	return s.checked
}

// Answers is everything a run decides, from the flags, the config, and the
// screens, in the values the plan and the apply take.
type Answers struct {
	Harnesses      []string
	SkillRepos     []string
	SkillRepoAsked bool
	ReposDirs      []string
	ReposDirsAsked bool
	Tools          map[string]bool
	Trackers       []TrackerAnswer
}

// Plan is what a run would do, ready to print and apply. questions is
// what the tracker screens asked, for the reasons the plan shows beside
// each answer; nil on the flag path, which asks nothing.
type Plan struct {
	picked    []harness.Harness
	current   plan
	trackers  []TrackerAnswer
	questions []TrackerQuestion
	embedded  assets
	noted     bool
}

// Plan works out what the answers mean for every harness, tool, and repo.
// Nothing on the machine changes.
func (s *Session) Plan(ctx context.Context, answers Answers) (*Plan, error) {
	picked, err := s.rows.ByKeys(answers.Harnesses)
	if err != nil {
		return nil, err
	}
	harnessPlans, err := planHarnesses(s.opts, s.embedded, s.gathered, picked)
	if err != nil {
		return nil, err
	}
	current := plan{harnesses: harnessPlans, catalog: s.gathered, repos: planRepos(s.opts.Home, answers.ReposDirs, s.skippedEarlier()), tools: append([]toolStatus(nil), s.toolStatuses(ctx)...)}
	for index := range current.tools {
		status := &current.tools[index]
		status.install = status.actionable() && answers.Tools[status.tool.Title]
	}
	markSkillPresence(s.opts, picked, current.tools)
	return &Plan{picked: picked, current: current, trackers: answers.Trackers, questions: s.scanned, embedded: s.embedded}, nil
}

// Print shows the plan the way the report shows the outcome: each harness,
// the tools, the repos, and what the answers say about each undeclared
// one.
func (p *Plan) Print(out io.Writer, opts Options, answers Answers) {
	printPlan(out, opts.Home, p.embedded, p.current)
	p.noted = noteInstallFolderOffPath(opts, out)
	printReposPlan(out, opts.Home, p.current.repos)
	printTrackerAnswers(out, answers.Trackers, p.questions)
}

// Pending reports whether applying the plan would change anything beyond
// setup's own files: a skill or letter to write, a tool to install or
// update, a tool skill to put in place, a tracker line to write.
func (p *Plan) Pending() bool {
	for _, entry := range p.current.harnesses {
		if entry.skills.Pending() || entry.letter.Outcome != letter.Same {
			return true
		}
	}
	for _, status := range p.current.tools {
		if status.install || status.present && status.tool.SkillInstall != "" && !status.skillPresent {
			return true
		}
	}
	for _, answer := range p.trackers {
		if !answer.Skip {
			return true
		}
	}
	return false
}

// Apply writes everything the plan says, in the order the report shows:
// the harnesses, then the config, then the tools, then the repos. The
// harnesses and the config come first so a tool or repo step that fails
// leaves the skills and the letter in place and is reported at the end.
func (s *Session) Apply(ctx context.Context, p *Plan, answers Answers) error {
	opts := s.opts
	out := opts.Stdout
	if len(p.picked) == 0 {
		fmt.Fprintln(out, "\nNo harness picked. Nothing changed in the harnesses. Pass --harness claude,codex to name one.")
		return nil
	}
	backupRoot, err := reserveBackup(opts.Home, opts.Now().Format("20060102-150405"), p.current)
	if err != nil {
		return err
	}
	if err := applyHarnesses(opts, s.embedded, p.current, backupRoot); err != nil {
		return err
	}
	saved := Config{
		Harnesses:       harness.Keys(p.picked),
		HarnessesFound:  s.everFound(),
		SkillRepos:      answers.SkillRepos,
		SkillReposAsked: answers.SkillRepoAsked,
		SkillOverrides:  rememberOverrides(s.config.SkillOverrides, p.current.catalog.unreachable(), p.current.catalog.picks),
		ReposDirs:       answers.ReposDirs,
		ReposDirsAsked:  answers.ReposDirsAsked,
		TrackersSkipped: rememberSkips(p.current.repos, answers.Trackers),
		Origins:         s.rememberOrigins(),
	}
	if err := saveConfig(opts.Home, saved); err != nil {
		return err
	}
	if err := writeScripts(s.embedded, opts.Home); err != nil {
		return err
	}
	toolsErr := applyTools(ctx, opts, p.current, p.picked, backupRoot)
	trackersErr := applyTrackers(ctx, opts, p.current.repos, answers.Trackers)
	if !p.noted {
		noteInstallFolderOffPath(opts, out)
	}
	fmt.Fprintf(out, "\nharness picks saved to %s\nrestart the harness so the skills load.\n", display(opts.Home, configPath(opts.Home)))
	return errors.Join(toolsErr, trackersErr)
}

// everFound is every harness found on this run or any earlier one, in
// table order, so a harness whose folder is gone for one run is still
// known and never offered as new when it's back.
func (s *Session) everFound() []string {
	known := map[string]bool{}
	for _, key := range append(append([]string{}, s.config.HarnessesFound...), s.found...) {
		known[key] = true
	}
	var keys []string
	for _, entry := range s.rows {
		if known[entry.Key] {
			keys = append(keys, entry.Key)
		}
	}
	return keys
}

// Close lets go of the clone folders the session holds open.
func (s *Session) Close() {
	if s.gatheredYet {
		s.gathered.close()
	}
}

// checkTools runs each tool's check, finds where the person runs it, runs
// its version line there, looks up the latest versions, and for each
// outdated tool works out who updates it.
func checkTools(ctx context.Context, opts Options, list []tools.Tool) []toolStatus {
	var statuses []toolStatus
	checking := counting(opts.Progress, "checking tools", len(list))
	for _, tool := range list {
		status := toolStatus{tool: tool, present: opts.Shell(ctx, tool.Check, io.Discard) == nil}
		if status.present {
			status.locate(ctx, opts)
			status.installed = installedVersion(ctx, opts, status)
		}
		statuses = append(statuses, status)
		checking.finished()
	}
	lookupLatest(ctx, opts, statuses)
	where := &locator{ctx: ctx, opts: opts}
	for index := range statuses {
		if statuses[index].outdated() {
			found := where.owned(statuses[index])
			statuses[index].owner, statuses[index].formula = found.owner, found.formula
		}
	}
	return statuses
}
