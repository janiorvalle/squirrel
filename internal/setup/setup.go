// Package setup is the whole flow: detect harnesses, fetch the skills repos
// the human named, plan what the skills, the letter, and the tools need,
// apply, and report. Run is the path without a terminal, driven by flags;
// the guided flow reads its facts and hands its answers through a Session,
// so the same plan and the same apply serve both.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/janiorvalle/squirrel/internal/harness"
	"github.com/janiorvalle/squirrel/internal/letter"
	"github.com/janiorvalle/squirrel/internal/skills"
	"github.com/janiorvalle/squirrel/internal/tools"
)

// Shell runs one shell command, streaming its output to output. A nil error
// means exit status zero.
type Shell func(ctx context.Context, command string, output io.Writer) error

// Latest finds the newest published version of a tool, as "v1.2.3".
type Latest func(ctx context.Context, tool tools.Tool) (string, error)

// Options is everything Run needs from the outside world. Stdin is the
// terminal the guided flow reads keys from; Run never reads it.
type Options struct {
	Files            fs.FS
	Home             string
	Getenv           func(string) string
	Harness          string
	InstallTools     bool
	UpdateTools      bool
	KeepInstructions bool
	Yes              bool
	SkillRepos       []string
	ForgetSkillRepos []string
	NoSkillRepo      bool
	ReposDirs        []string
	AskTrackersAgain bool
	Overrides        map[string]string
	Stdin            io.Reader
	Stdout           io.Writer
	Shell            Shell
	Latest           Latest
	Now              func() time.Time
	// Progress takes each step of a wait between two screens as it moves,
	// for a terminal to show a count while the tracker scan runs. Nil
	// shows nothing, and the flag path passes nil.
	Progress func(Progress)
}

type assets struct {
	skills   fs.FS
	scripts  fs.FS
	letter   string
	tools    []tools.Tool
	vendored []string
}

type harnessPlan struct {
	harness      harness.Harness
	skills       skills.Plan
	instructions string
	letter       letter.Change
}

// toolStatus is one tool as found on the machine. installed and latest are
// "v1.2.3" or "" when unknown; a tool with no Version line never has either.
// path is where a present tool's binary is, and onOwnPath whether the
// person's own PATH finds it there, which decides where the lines that
// run the tool run. owner and formula are who put it there, read only for
// outdated tools since only the update offer depends on them. install is
// the agreed action: an install when missing, an update when outdated.
type toolStatus struct {
	tool         tools.Tool
	present      bool
	installed    string
	latest       string
	path         string
	onOwnPath    bool
	owner        owner
	formula      string
	skillPresent bool
	install      bool
}

// outdated is true when the installed version is behind the latest. A pinned
// tool is outdated at anything but the pin, ahead or unknown included: the
// pin is the version that was reviewed, and its install line puts it back.
// A pinned tool with no Version line has latest "" and installed "", so it
// is never outdated, the same as any tool setup can't read a version from.
func (status toolStatus) outdated() bool {
	if !status.present {
		return false
	}
	if status.tool.Pin != "" {
		return status.installed != status.latest
	}
	return tools.Outdated(status.installed, status.latest)
}

// actionable is true when setup has a line it could run for this tool: the
// install line when it's missing, the owner's update line when it's
// outdated.
func (status toolStatus) actionable() bool {
	return status.line() != "" && (!status.present || status.outdated())
}

// line is the command setup would run for this tool: the install line when
// it's missing, the update line when it's present.
func (status toolStatus) line() string {
	if !status.present {
		return status.tool.Command
	}
	if status.tool.Command == "" {
		return ""
	}
	return status.update()
}

type plan struct {
	harnesses []harnessPlan
	tools     []toolStatus
	catalog   catalog
	repos     reposReport
}

type pickSource int

const (
	fromDetect pickSource = iota
	fromConfig
	fromFlag
)

// Run prints the plan and, with Yes, applies it with what the flags say.
// Without Yes it changes nothing in the harnesses and prints the flags that
// would; a skills repo named earlier is still refreshed.
func Run(ctx context.Context, opts Options) error {
	session, err := Start(opts)
	if err != nil {
		return err
	}
	defer session.Close()
	picked, _, err := choose(opts, session.rows, session.config)
	if err != nil {
		return err
	}
	out := opts.Stdout
	printHarnesses(out, opts.Home, session.rows, picked)
	repoNames, err := session.SkillRepos()
	if err != nil {
		return err
	}
	asked := session.SkillRepoAsked()
	reposDirs, reposAsked, err := chooseReposDirs(session.config, opts)
	if err != nil {
		return err
	}
	open, err := session.Gather(ctx, repoNames)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		return refusal(open[0])
	}
	if err := session.PickSources(nil); err != nil {
		return err
	}
	answers := Answers{
		Harnesses:      harness.Keys(picked),
		SkillRepos:     repoNames,
		SkillRepoAsked: asked,
		ReposDirs:      reposDirs,
		ReposDirsAsked: reposAsked,
		Tools:          map[string]bool{},
	}
	for _, choice := range session.Tools(ctx) {
		answers.Tools[choice.Title] = choice.Checked
	}
	current, err := session.Plan(ctx, answers)
	if err != nil {
		return err
	}
	current.Print(out, opts, answers)
	if !opts.Yes {
		printRerun(out, opts, picked, current.current, asked)
		return nil
	}
	return session.Apply(ctx, current, answers)
}

func loadAssets(files fs.FS) (assets, error) {
	skillsFS, err := fs.Sub(files, "skills")
	if err != nil {
		return assets{}, fmt.Errorf("[SQUIRREL-EMBED] the binary has no skills folder embedded: %w; reinstall it", err)
	}
	scriptsFS, err := fs.Sub(files, "scripts")
	if err != nil {
		return assets{}, fmt.Errorf("[SQUIRREL-EMBED] the binary has no scripts folder embedded: %w; reinstall it", err)
	}
	letterText, err := fs.ReadFile(files, "AGENTS.md")
	if err != nil {
		return assets{}, fmt.Errorf("[SQUIRREL-EMBED] the binary has no AGENTS.md embedded: %w; reinstall it", err)
	}
	toolsText, err := fs.ReadFile(files, "tools.md")
	if err != nil {
		return assets{}, fmt.Errorf("[SQUIRREL-EMBED] the binary has no tools.md embedded: %w; reinstall it", err)
	}
	vendorText, err := fs.ReadFile(files, "vendor.json")
	if err != nil {
		return assets{}, fmt.Errorf("[SQUIRREL-EMBED] the binary has no vendor.json embedded: %w; reinstall it", err)
	}
	var vendor struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(vendorText, &vendor); err != nil {
		return assets{}, fmt.Errorf("[SQUIRREL-EMBED] the embedded vendor.json is not valid JSON: %w; rebuild the binary from a checkout where make verify passes", err)
	}
	var vendored []string
	for _, entry := range vendor.Skills {
		vendored = append(vendored, entry.Name)
	}
	return assets{skills: skillsFS, scripts: scriptsFS, letter: string(letterText), tools: tools.Parse(string(toolsText)), vendored: vendored}, nil
}

func choose(opts Options, rows harness.Table, config Config) ([]harness.Harness, pickSource, error) {
	if opts.Harness != "" {
		picked, err := rows.Parse(opts.Harness)
		return picked, fromFlag, err
	}
	if len(config.Harnesses) > 0 {
		picked, err := rows.ByKeys(config.Harnesses)
		if err != nil {
			return nil, fromConfig, fmt.Errorf("%w; the saved picks in %q are stale, pass --harness to replace them", err, configPath(opts.Home))
		}
		return picked, fromConfig, nil
	}
	return rows.Found(), fromDetect, nil
}

// planRepos scans the named folders, holding the checkouts skipped on an
// earlier run. With none named, the guesses under home go in the report
// instead, so the hint can say what setup would have scanned.
func planRepos(home string, reposDirs []string, skipped map[string]bool) reposReport {
	report := reposReport{dirs: reposDirs, repos: scanRepos(reposDirs, skipped)}
	if len(reposDirs) == 0 {
		report.guesses = guessReposDirs(home)
	}
	return report
}

// installedVersion is what the tool the person runs prints: the version
// line runs on their own PATH when the binary is there, since the shell's
// puts ~/.local/bin ahead of it and a stale copy there would answer for
// the Homebrew binary they use, and on the shell's when it isn't, the
// just-installed case.
func installedVersion(ctx context.Context, opts Options, status toolStatus) string {
	if status.tool.Version == "" {
		return ""
	}
	return versionPrinted(ctx, opts, status.ownLine(opts, status.tool.Version))
}

// runToolLine runs a line that runs the tool itself, its skill install,
// where the person's own PATH finds the binary, so it's the binary they
// run that installs its skill, and a failure there is the failure.
func runToolLine(ctx context.Context, opts Options, status toolStatus, command string) error {
	return opts.Shell(ctx, status.ownLine(opts, command), io.Discard)
}

func versionPrinted(ctx context.Context, opts Options, line string) string {
	var output bytes.Buffer
	if err := opts.Shell(ctx, line, &output); err != nil {
		return ""
	}
	return tools.ParseVersion(output.String())
}

// lookupLatest asks each source at the same time, so a network that drops
// packets costs one timeout, not one per tool. Missing tools are looked up
// too: after an install the report compares what landed against it. A
// pinned tool's latest is its pin, no lookup: the registry may be further
// along, but the pin is what the install line installs.
func lookupLatest(ctx context.Context, opts Options, statuses []toolStatus) {
	var lookups []*toolStatus
	for index := range statuses {
		if statuses[index].tool.Version == "" {
			continue
		}
		if pin := statuses[index].tool.Pin; pin != "" {
			statuses[index].latest = pin
			continue
		}
		lookups = append(lookups, &statuses[index])
	}
	looking := counting(opts.Progress, "looking up latest versions", len(lookups))
	var wait sync.WaitGroup
	for _, status := range lookups {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if latest, err := opts.Latest(ctx, status.tool); err == nil {
				status.latest = latest
			}
			looking.finished()
		}()
	}
	wait.Wait()
}

func agreedByFlag(opts Options, status toolStatus) bool {
	if !status.actionable() {
		return false
	}
	if status.present {
		return opts.UpdateTools
	}
	return opts.InstallTools
}

func planHarnesses(opts Options, embedded assets, skillSources catalog, picked []harness.Harness) ([]harnessPlan, error) {
	var plans []harnessPlan
	for _, entry := range picked {
		skillPlan, err := skills.PlanFor(skillSources.sources, skillSources.picks, entry.SkillsDir())
		if err != nil {
			return nil, err
		}
		path := entry.InstructionsPath()
		existing, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("[SQUIRREL-LETTER-READ] cannot read %q: %w; make it readable and rerun", path, err)
		}
		plans = append(plans, harnessPlan{
			harness:      entry,
			skills:       skillPlan,
			instructions: string(existing),
			letter:       letter.Plan(string(existing), embedded.letter, entry.Lead, opts.KeepInstructions),
		})
	}
	return plans, nil
}

func markSkillPresence(opts Options, picked []harness.Harness, statuses []toolStatus) {
	for index := range statuses {
		if folder := statuses[index].tool.SkillFolder; folder != "" {
			statuses[index].skillPresent = skillPresent(opts, picked, folder)
		}
	}
}

// skillPresent is true when every picked harness has the tool's skill as the
// tool wrote it. A copy that differs is one an update left behind, or one a
// failed copy left in place, and counts as missing so the tool's install runs
// again and the copy is replaced. So does a copy with nothing left to compare
// it with, the tool's own folders deleted: the install runs again and writes
// them back. A copy in the shared ~/.agents/skills alone is a source like
// the others, not presence everywhere: Claude Code and Codex don't read
// that folder, and a copy an older tool left there would hide a newer one.
func skillPresent(opts Options, picked []harness.Harness, folder string) bool {
	skill, found := toolSkill(opts, folder)
	if !found {
		return false
	}
	for _, entry := range picked {
		target := filepath.Join(entry.SkillsDir(), folder)
		if !isDir(target) {
			return false
		}
		if target == skill.source {
			continue
		}
		if same, err := skills.Same(skill.Skill, target); err != nil || !same {
			return false
		}
	}
	return true
}

// installedSkill is a tool's skill as the tool's own install left it on
// disk, in the shape the skills package reads, and the folder it came from.
type installedSkill struct {
	skills.Skill
	source string
}

// toolSkill is the folder a tool's skill install wrote last, by the time on
// its SKILL.md, among the folders the tools write to: ~/.agents/skills and
// the harnesses the table marks, Claude Code's and Codex's, picked or not.
// A folder an older version of the tool left in one of them is older than
// what the install just wrote, so it never wins. A folder setup copied into
// is never the source, so a copy changed by hand is replaced, not mirrored.
func toolSkill(opts Options, folder string) (installedSkill, bool) {
	candidates := []string{filepath.Join(opts.Home, ".agents", "skills", folder)}
	for _, entry := range harness.Resolve(opts.Home, opts.Getenv) {
		if entry.ToolSkills {
			candidates = append(candidates, filepath.Join(entry.SkillsDir(), folder))
		}
	}
	var newest string
	var written time.Time
	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Join(candidate, "SKILL.md"))
		if err != nil || newest != "" && !info.ModTime().After(written) {
			continue
		}
		newest, written = candidate, info.ModTime()
	}
	if newest == "" {
		return installedSkill{}, false
	}
	return installedSkill{
		Skill:  skills.Skill{Name: folder, Source: skills.Source{Name: folder, Files: os.DirFS(filepath.Dir(newest))}},
		source: newest,
	}, true
}

// carrySkill puts a tool's skill into each picked harness as the tool's own
// install left it and returns the harnesses whose copy changed. The tools
// write their skill into Claude Code's and Codex's folders, some also into
// ~/.agents/skills, so a person who picked OpenCode or Pi would otherwise
// never get it there and every run would find it missing and install it
// again. The copy is the tool's, not squirrel's: no source owns the folder, so
// the skills plan leaves it alone as local, and a copy left behind by an
// update is replaced and backed up like any changed skill.
func carrySkill(opts Options, picked []harness.Harness, backupRoot, folder string) ([]harness.Harness, error) {
	skill, found := toolSkill(opts, folder)
	if !found {
		return nil, nil
	}
	var copied []harness.Harness
	for _, entry := range picked {
		if filepath.Join(entry.SkillsDir(), folder) == skill.source {
			continue
		}
		changed, err := skills.Sync(skill.Skill, entry.SkillsDir(), filepath.Join(backupRoot, entry.Key, "skills"))
		if err != nil {
			return copied, err
		}
		if changed {
			copied = append(copied, entry)
		}
	}
	return copied, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printHarnesses(out io.Writer, home string, rows harness.Table, picked []harness.Harness) {
	fmt.Fprintln(out, "harnesses")
	for _, entry := range rows {
		mark, state := " ", "not found"
		if entry.Installed() {
			state = "found"
		}
		if contains(picked, entry) {
			mark = "x"
		}
		fmt.Fprintf(out, "  [%s] %-11s %s, %s\n", mark, entry.Name, rootWithVariable(home, entry), state)
	}
}

func printPlan(out io.Writer, home string, embedded assets, current plan) {
	for _, entry := range current.harnesses {
		fmt.Fprintf(out, "\n%s  %s\n", entry.harness.Name, display(home, entry.harness.SkillsDir()))
		fmt.Fprintf(out, "  new      %s\n", skillList(entry.skills.New))
		fmt.Fprintf(out, "  changed  %s\n", skillList(entry.skills.Changed))
		fmt.Fprintf(out, "  same     %d skills\n", len(entry.skills.Same))
		fmt.Fprintf(out, "  local    %s (untouched)\n", list(entry.skills.Local))
		fmt.Fprintf(out, "  letter   %s %s\n", display(home, entry.harness.InstructionsPath()), letterIntent(entry.letter.Outcome))
	}
	if len(embedded.vendored) > 0 {
		fmt.Fprintf(out, "\nvendored %s (upstream text, pinned in vendor.json)\n", list(embedded.vendored))
	}
	fmt.Fprintln(out, "\ntools")
	for _, status := range current.tools {
		fmt.Fprintf(out, "  %s\n", toolIntent(status))
	}
}

func letterIntent(outcome letter.Outcome) string {
	switch outcome {
	case letter.Create:
		return "would be created"
	case letter.Update:
		return "would get the new letter between the markers"
	case letter.Replace:
		return "has other content: would be replaced by the letter and backed up"
	case letter.Append:
		return "would get the letter appended"
	}
	return "up to date"
}

func toolIntent(status toolStatus) string {
	line := toolState(status)
	if !status.present || status.tool.SkillInstall == "" || status.tool.SkillFolder == "" {
		return line
	}
	if status.skillPresent {
		return line + ", skill present"
	}
	return line + ", skill missing, would run: " + status.tool.SkillInstall
}

// toolState names one of the three states, missing, outdated, or current,
// with the versions that decided it. Latest unknown is current with a note:
// setup never blocks on a lookup. A missing tool with no line setup could
// run, a prerequisite anywhere or a zip download on Windows, says so and
// where to look.
func toolState(status toolStatus) string {
	switch {
	case !status.present && status.tool.Command == "":
		return fmt.Sprintf("missing %s, which setup doesn't install. get it by hand, %s", status.tool.Title, status.tool.Install)
	case !status.present:
		return fmt.Sprintf("missing %s. install: %s", status.tool.Title, status.tool.Install)
	case status.outdated() && status.update() == "":
		return fmt.Sprintf("%s %s %s, %s %s, at %s, which setup didn't put there; update it the way it was installed", offset(status), status.tool.Title, installedLabel(status), reference(status), tools.Display(status.latest), status.path)
	case status.outdated():
		return fmt.Sprintf("%s %s %s, %s %s. update: %s", offset(status), status.tool.Title, installedLabel(status), reference(status), tools.Display(status.latest), status.updateShown())
	case status.tool.Version == "":
		return "ok " + status.tool.Title
	case status.installed == "":
		return "ok " + status.tool.Title + ", version unknown"
	case status.latest == "":
		return fmt.Sprintf("ok %s %s, latest unknown", status.tool.Title, tools.Display(status.installed))
	}
	return fmt.Sprintf("ok %s %s", status.tool.Title, tools.Display(status.installed))
}

// toolOffer is the line the tools screen shows for an actionable tool: the
// install for a missing one, the update for an outdated one, with the line
// that would run.
func toolOffer(status toolStatus) string {
	if !status.present {
		return fmt.Sprintf("install %s: %s", status.tool.Title, status.line())
	}
	return fmt.Sprintf("update %s %s to %s: %s", status.tool.Title, installedLabel(status), tools.Display(status.latest), status.line())
}

// offset says which way the installed version is off: ahead when a pinned
// tool is past its pin, outdated when behind it or unreadable.
func offset(status toolStatus) string {
	if tools.Outdated(status.latest, status.installed) {
		return "ahead"
	}
	return "outdated"
}

// installedLabel is the installed version as the tool prints it, or the words
// for a pinned tool whose version command printed none.
func installedLabel(status toolStatus) string {
	if status.installed == "" {
		return "version unknown"
	}
	return tools.Display(status.installed)
}

// reference names what the installed version was compared with.
func reference(status toolStatus) string {
	if status.tool.Pin != "" {
		return "pinned"
	}
	return "latest"
}

func printRerun(out io.Writer, opts Options, picked []harness.Harness, current plan, repoAsked bool) {
	fmt.Fprintln(out, "\nNo terminal, so nothing changed in the harnesses. Rerun with the flags to apply:")
	keys := strings.Join(harness.Keys(picked), ",")
	if keys == "" {
		keys = "claude,codex"
	}
	line := "squirrel setup --harness " + keys + " --yes"
	if opts.InstallTools {
		line += " --install-tools"
	}
	if opts.UpdateTools {
		line += " --update-tools"
	}
	if opts.KeepInstructions {
		line += " --keep-instructions"
	}
	for _, repo := range opts.SkillRepos {
		line += " --skill-repo " + repo
	}
	for _, repo := range opts.ForgetSkillRepos {
		line += " --forget-skill-repo " + repo
	}
	for _, name := range sortedKeys(opts.Overrides) {
		line += " --override " + name + "=" + opts.Overrides[name]
	}
	if opts.NoSkillRepo {
		line += " --no-skill-repo"
	}
	for _, dir := range opts.ReposDirs {
		line += " --repos-dir " + quote(runtime.GOOS, dir)
	}
	fmt.Fprintf(out, "  %s\n", line)
	if !repoAsked {
		fmt.Fprintln(out, "  add --skill-repo owner/name to also install the skills from a repo of yours, or --no-skill-repo to say there is none")
	}
	if undeclared := current.repos.undeclared(); len(undeclared) > 0 {
		fmt.Fprintf(out, "  %d repo(s) declare no tracker: %s; rerun with a terminal to name each one\n", len(undeclared), repoNames(undeclared))
	}
	missing, outdated := false, false
	for _, status := range current.tools {
		if !status.actionable() {
			continue
		}
		if status.present {
			outdated = true
		} else {
			missing = true
		}
	}
	if missing && !opts.InstallTools {
		fmt.Fprintln(out, "  add --install-tools to also install the missing tools")
	}
	if outdated && !opts.UpdateTools {
		fmt.Fprintln(out, "  add --update-tools to also update the outdated tools")
	}
	for _, entry := range current.harnesses {
		if !opts.KeepInstructions && entry.letter.Outcome == letter.Replace && entry.harness.Lead == "" {
			fmt.Fprintf(out, "  add --keep-instructions to append the letter to %s instead of replacing it\n", display(opts.Home, entry.harness.InstructionsPath()))
		}
	}
}

// reserveBackup picks the backup folder for this run. When something will be
// backed up the folder is created exclusively, so two runs started in the same
// second never share one; otherwise the path is only named, never created.
func reserveBackup(home, stamp string, current plan) (string, error) {
	parent := filepath.Join(home, ".squirrel", "backup")
	needed := toolSkillCopyWillBeReplaced(current)
	for _, entry := range current.harnesses {
		if len(entry.skills.Changed) > 0 || entry.letter.Outcome == letter.Replace {
			needed = true
		}
	}
	if !needed {
		return filepath.Join(parent, stamp), nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("[SQUIRREL-BACKUP-DIR] cannot create %q: %w; make the home folder writable and rerun", parent, err)
	}
	for attempt := 1; ; attempt++ {
		name := stamp
		if attempt > 1 {
			name = fmt.Sprintf("%s-%d", stamp, attempt)
		}
		root := filepath.Join(parent, name)
		err := os.Mkdir(root, 0o755)
		if err == nil {
			return root, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("[SQUIRREL-BACKUP-DIR] cannot create %q: %w; make %q writable and rerun", root, err, parent)
		}
	}
}

// toolSkillCopyWillBeReplaced is true when a tool's skill install will run
// and a picked harness already has that skill, so the copy there may be
// replaced and needs the backup folder.
func toolSkillCopyWillBeReplaced(current plan) bool {
	for _, status := range current.tools {
		if status.tool.SkillFolder == "" || !(status.install || status.present && !status.skillPresent) {
			continue
		}
		for _, entry := range current.harnesses {
			if isDir(filepath.Join(entry.harness.SkillsDir(), status.tool.SkillFolder)) {
				return true
			}
		}
	}
	return false
}

func applyHarnesses(opts Options, embedded assets, current plan, backupRoot string) error {
	out := opts.Stdout
	for _, entry := range current.harnesses {
		backup := filepath.Join(backupRoot, entry.harness.Key)
		fmt.Fprintf(out, "\n%s\n", entry.harness.Name)
		if err := applySkills(current.catalog, entry, opts.Home, backup, out); err != nil {
			return err
		}
		if err := applyLetter(opts, embedded, entry, backup); err != nil {
			return err
		}
	}
	return nil
}

// applyTools installs what was agreed and reports every tool. The skills and
// the letter are already in place, so a tool that fails is reported at the end
// instead of stopping the run.
func applyTools(ctx context.Context, opts Options, current plan, picked []harness.Harness, backupRoot string) error {
	fmt.Fprintln(opts.Stdout, "\ntools")
	var failures []error
	for _, status := range current.tools {
		if err := applyTool(ctx, opts, status, picked, backupRoot); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("[SQUIRREL-TOOLS] the skills and the letter are in place, but %d tool step(s) failed:\n%w", len(failures), errors.Join(failures...))
}

// noteInstallFolderOffPath says that ~/.local/bin exists but is not on the
// PATH this process started with, and reports whether it did. The shell that
// runs the tools.md lines puts the folder on PATH itself, which is why a tool
// there is found and why its installer printed no hint. The person's next
// terminal has no such help, so setup says it on every run until the profile
// does: after the plan, so a run that only prints the plan says it too, and
// again after the tools when the first install of the run created the folder.
func noteInstallFolderOffPath(opts Options, out io.Writer) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	folder := filepath.Join(opts.Home, ".local", "bin")
	if _, err := os.Stat(folder); err != nil {
		return false
	}
	for _, entry := range filepath.SplitList(opts.Getenv("PATH")) {
		if entry == folder {
			return false
		}
	}
	fmt.Fprintf(out, "  %s is not on PATH; setup looks there, a new terminal won't until this line is in your shell profile, ~/.zshrc or ~/.bashrc (fish: fish_add_path ~/.local/bin):\n    export PATH=\"$HOME/.local/bin:$PATH\"\n", display(opts.Home, folder))
	return true
}

func applySkills(skillSources catalog, entry harnessPlan, home, backup string, out io.Writer) error {
	dest := entry.harness.SkillsDir()
	if !entry.skills.Pending() {
		fmt.Fprintf(out, "  skills   up to date in %s\n", display(home, dest))
		return nil
	}
	skillsBackup := filepath.Join(backup, "skills")
	if err := skills.Apply(dest, entry.skills, skillsBackup); err != nil {
		return err
	}
	fmt.Fprintf(out, "  skills   %d installed, %d updated in %s\n", len(entry.skills.New), len(entry.skills.Changed), display(home, dest))
	if len(entry.skills.Changed) > 0 {
		fmt.Fprintf(out, "  backup   %s\n", display(home, skillsBackup))
	}
	after, err := skills.PlanFor(skillSources.sources, skillSources.picks, dest)
	if err != nil {
		return err
	}
	if after.Pending() {
		fmt.Fprintf(out, "  remaining drift: %s\n", skillList(append(after.New, after.Changed...)))
	}
	return nil
}

// applyLetter reads the file again first: the prompts sat between the plan
// and this point, and a file another session changed meanwhile gets a fresh
// plan instead of the stale content.
func applyLetter(opts Options, embedded assets, entry harnessPlan, backup string) error {
	out := opts.Stdout
	home := opts.Home
	path := entry.harness.InstructionsPath()
	shown := display(home, path)
	fresh, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("[SQUIRREL-LETTER-READ] cannot read %q: %w; make it readable and rerun", path, err)
	}
	change := entry.letter
	if string(fresh) != entry.instructions {
		change = letter.Plan(string(fresh), embedded.letter, entry.harness.Lead, opts.KeepInstructions)
		fmt.Fprintf(out, "  letter   %s changed since the plan, planned again\n", shown)
	}
	if change.Outcome == letter.Same {
		fmt.Fprintf(out, "  letter   up to date in %s\n", shown)
		return nil
	}
	if change.Outcome == letter.Replace {
		saved := filepath.Join(backup, filepath.Base(path))
		if err := copyFile(path, saved); err != nil {
			return fmt.Errorf("[SQUIRREL-LETTER-BACKUP] cannot back up %q to %q: %w; the file was not changed, fix the permissions and rerun", path, saved, err)
		}
		fmt.Fprintf(out, "  letter   replaced %s, old file backed up to %s\n", shown, display(home, saved))
	} else {
		fmt.Fprintf(out, "  letter   %s %s\n", letterPast(change.Outcome), shown)
	}
	return writeFile(path, change.Content)
}

func letterPast(outcome letter.Outcome) string {
	switch outcome {
	case letter.Create:
		return "created"
	case letter.Append:
		return "appended to"
	}
	return "updated between the markers in"
}

func applyTool(ctx context.Context, opts Options, status toolStatus, picked []harness.Harness, backupRoot string) error {
	out := opts.Stdout
	if !status.present && !status.install {
		fmt.Fprintf(out, "  %s\n", toolState(status))
		return nil
	}
	if status.install {
		if err := runInstall(ctx, opts, &status, out); err != nil {
			return err
		}
	}
	line := "  " + toolState(status)
	if status.tool.SkillInstall == "" || status.tool.SkillFolder == "" {
		fmt.Fprintln(out, line)
		return nil
	}
	// A skill ships with its binary, so an update reinstalls it even when a
	// copy is already there: the old copy describes the old binary.
	if status.skillPresent && !status.install {
		fmt.Fprintln(out, line+", skill present")
		return nil
	}
	if err := runToolLine(ctx, opts, status, status.tool.SkillInstall); err != nil {
		fmt.Fprintf(out, "%s, skill install FAILED via %s: %v\n", line, status.tool.SkillInstall, err)
		return fmt.Errorf("%s: `%s` failed: %v; run it by hand so the tool's skill is in place", status.tool.Title, status.tool.SkillInstall, err)
	}
	line += ", skill installed via " + status.tool.SkillInstall
	copied, err := carrySkill(opts, picked, backupRoot, status.tool.SkillFolder)
	if len(copied) > 0 {
		line += ", copied to " + names(copied)
	}
	if err != nil {
		fmt.Fprintf(out, "%s, copy FAILED: %v\n", line, err)
		return fmt.Errorf("%s: %w", status.tool.Title, err)
	}
	fmt.Fprintln(out, line)
	return nil
}

// runInstall runs the tool's install line for a missing or an outdated tool,
// then reads the check and the version again so the report shows what is
// on the machine now, not what the plan expected.
func runInstall(ctx context.Context, opts Options, status *toolStatus, out io.Writer) error {
	tool := status.tool
	verb, done := "installing "+tool.Title, "installed"
	if status.present {
		verb, done = fmt.Sprintf("updating %s %s to %s", tool.Title, installedLabel(*status), tools.Display(status.latest)), "updated"
	}
	// An update runs where the person's PATH found the binary, so every
	// command in the line, "npm install -g x && x install", runs the binary
	// they run and not a stale copy the shell's PATH puts first.
	command := status.line()
	fmt.Fprintf(out, "  %s: %s\n", verb, command)
	if err := opts.Shell(ctx, status.ownLine(opts, command), out); err != nil {
		fmt.Fprintf(out, "  FAILED %s: %v\n", tool.Title, err)
		return fmt.Errorf("%s: `%s` failed: %v; run it by hand, then rerun squirrel setup", tool.Title, command, err)
	}
	if err := opts.Shell(ctx, tool.Check, io.Discard); err != nil {
		fmt.Fprintf(out, "  FAILED %s: %s, but its check still fails\n", tool.Title, done)
		return fmt.Errorf("%s: `%s` ran, but the check `%s` still fails; read the install output above: if the download failed, run the install line again; if it put %s in a folder that is not on PATH, add that folder to PATH in your shell profile and open a new terminal; then rerun squirrel setup", tool.Title, command, tool.Check, tool.Title)
	}
	status.present = true
	status.locate(ctx, opts)
	status.installed = installedVersion(ctx, opts, *status)
	if status.outdated() && status.installed == "" {
		fmt.Fprintf(out, "  FAILED %s: %s, but `%s` prints no version\n", tool.Title, done, tool.Version)
		return fmt.Errorf("%s: `%s` ran, but `%s` prints no version, so the pinned %s can't be confirmed; run it by hand and fix what it prints, then rerun squirrel setup", tool.Title, command, tool.Version, tools.Display(status.latest))
	}
	if status.outdated() && status.owner == byHomebrew {
		fmt.Fprintf(out, "  FAILED %s: %s, but `%s` still prints %s\n", tool.Title, done, tool.Version, tools.Display(status.installed))
		return fmt.Errorf("%s: `%s` ran, but `%s` still prints %s and the latest is %s; Homebrew's formula is behind the release, rerun squirrel setup once `brew upgrade` has it", tool.Title, command, tool.Version, tools.Display(status.installed), tools.Display(status.latest))
	}
	if status.outdated() {
		fmt.Fprintf(out, "  FAILED %s: %s, but `%s` still prints %s\n", tool.Title, done, tool.Version, tools.Display(status.installed))
		return fmt.Errorf("%s: `%s` ran, but `%s` still prints %s and the %s is %s; another %s earlier on PATH is winning, remove it or put the new one first, then rerun squirrel setup", tool.Title, command, tool.Version, tools.Display(status.installed), reference(*status), tools.Display(status.latest), tool.Title)
	}
	if status.installed == "" {
		fmt.Fprintf(out, "  %s %s\n", done, tool.Title)
		return nil
	}
	fmt.Fprintf(out, "  %s %s %s\n", done, tool.Title, tools.Display(status.installed))
	return nil
}

func copyFile(source, destination string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, content, 0o600)
}

// writeFile replaces path in one step: the new content lands in a temporary
// file beside it and is renamed over the original only once fully written, so
// a failed write leaves the user's file as it was. A symlink, the dotfiles
// repo case, stays a symlink: the file it points to is what gets replaced.
func writeFile(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("[SQUIRREL-LETTER-WRITE] cannot create %q: %w; make the parent writable and rerun", filepath.Dir(path), err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".squirrel-letter-*")
	if err != nil {
		return fmt.Errorf("[SQUIRREL-LETTER-WRITE] cannot stage a new %q: %w; make its folder writable and rerun", path, err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	fail := func(step string, cause error) error {
		_ = temporary.Close()
		return fmt.Errorf("[SQUIRREL-LETTER-WRITE] cannot %s for %q: %w; the file was not changed, fix the folder and rerun", step, path, cause)
	}
	if err := temporary.Chmod(mode); err != nil {
		return fail("set permissions", err)
	}
	if _, err := temporary.WriteString(content); err != nil {
		return fail("write the new content", err)
	}
	if err := temporary.Sync(); err != nil {
		return fail("sync the new content", err)
	}
	if err := temporary.Close(); err != nil {
		return fail("close the staged file", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("[SQUIRREL-LETTER-WRITE] cannot replace %q: %w; the file was not changed, fix the permissions and rerun", path, err)
	}
	return nil
}

func rootWithVariable(home string, entry harness.Harness) string {
	shown := display(home, entry.Root)
	if entry.HomeVar != "" {
		return shown + " (" + entry.HomeVar + ")"
	}
	return shown
}

func display(home, path string) string {
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rest)
	}
	return path
}

func list(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

// skillList names skills, each with its source when that isn't squirrel.
func skillList(items []skills.Skill) string {
	if len(items) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(items))
	for _, skill := range items {
		part := skill.Name
		if skill.Source.Name != "squirrel" {
			part += " (" + skill.Source.Name + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func names(picked []harness.Harness) string {
	parts := make([]string, 0, len(picked))
	for _, entry := range picked {
		parts = append(parts, entry.Name)
	}
	return strings.Join(parts, ", ")
}

func contains(picked []harness.Harness, entry harness.Harness) bool {
	for _, candidate := range picked {
		if candidate.Key == entry.Key {
			return true
		}
	}
	return false
}
