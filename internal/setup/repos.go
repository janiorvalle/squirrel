package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/janiorvalle/squirrel/internal/skills"
)

// skillRepo is one skills repo the human named, as found on this run. Its
// clone lives under ~/.squirrel/repos/owner/name. verb says what happened to
// the clone; failure says why it can't be installed from this run, and a
// usable repo has none and carries its source. toolSkills are the folders
// in it that a tool installs itself and oddNames the folders that aren't
// lowercase names; both are left out of the source.
type skillRepo struct {
	name       string
	dir        string
	verb       string
	failure    string
	source     skills.Source
	count      int
	toolSkills []string
	oddNames   []string
	roots      []*os.Root
}

func (r skillRepo) usable() bool {
	return r.failure == ""
}

// catalog is every place skills come from this run, squirrel's embedded folder
// first and then each usable repo, and which source each name more than one
// of them holds is taken from. collisions is every such name; open is the
// ones the flags and the saved picks didn't settle, still to be asked.
type catalog struct {
	repos      []skillRepo
	sources    []skills.Source
	collisions []skills.Collision
	picks      map[string]string
	open       []skills.Collision
	held       []string
}

// close lets go of the clone folders. Windows won't remove a folder while
// a handle on it is open, and the roots hold one each.
func (c catalog) close() {
	for _, repo := range c.repos {
		for _, root := range repo.roots {
			root.Close()
		}
	}
}

// repoName normalizes what a person types or pastes for a repo to owner/name.
func repoName(spec string) (string, error) {
	name := strings.TrimSpace(spec)
	name = strings.TrimPrefix(name, "https://")
	name = strings.TrimPrefix(name, "github.com/")
	name = strings.TrimSuffix(name, ".git")
	name = strings.Trim(name, "/")
	owner, repo, ok := strings.Cut(name, "/")
	if !ok || !plainName(owner) || !plainName(repo) {
		return "", fmt.Errorf("[SQUIRREL-SKILL-REPO] %q is not owner/name; expected a GitHub repo such as janiorvalle/work-skills, with a skills/ folder holding one folder per skill", spec)
	}
	return owner + "/" + repo, nil
}

func plainName(part string) bool {
	if part == "" {
		return false
	}
	for _, character := range part {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '-', character == '_', character == '.':
		default:
			return false
		}
	}
	return true
}

// chooseRepos is the repos this run installs from: the saved ones, then the
// ones --skill-repo names, minus the ones --forget-skill-repo names.
func chooseRepos(saved []string, opts Options) ([]string, error) {
	forget := map[string]bool{}
	for _, spec := range opts.ForgetSkillRepos {
		name, err := repoName(spec)
		if err != nil {
			return nil, err
		}
		forget[name] = true
	}
	var repos []string
	seen := map[string]bool{}
	for _, spec := range append(append([]string{}, saved...), opts.SkillRepos...) {
		name, err := repoName(spec)
		if err != nil {
			return nil, err
		}
		if !seen[name] && !forget[name] {
			seen[name] = true
			repos = append(repos, name)
		}
	}
	return repos, nil
}

// syncRepos clones each repo that isn't on the machine yet and pulls each
// one that is, both through gh so its login is what reaches GitHub: plain
// git has no credentials for a private clone until someone runs `gh auth
// setup-git`, and would stop at a username prompt. A pull that fails keeps
// the copy from the last run, so a dead network never changes the plan; a
// clone that fails is reported and left out, and setup carries on.
func syncRepos(ctx context.Context, opts Options, names []string) []skillRepo {
	repos := make([]skillRepo, 0, len(names))
	for _, name := range names {
		repos = append(repos, syncRepo(ctx, opts, name))
	}
	return repos
}

func syncRepo(ctx context.Context, opts Options, name string) skillRepo {
	repo := skillRepo{name: name, dir: filepath.Join(opts.Home, ".squirrel", "repos", filepath.FromSlash(name))}
	command, verb, doing := "gh repo clone "+name+" "+quote(runtime.GOOS, repo.dir), "cloned", "cloning "+name
	if isDir(filepath.Join(repo.dir, ".git")) {
		command, verb, doing = pullLine(runtime.GOOS, name, repo.dir), "pulled", "pulling "+name
	} else if err := os.MkdirAll(filepath.Dir(repo.dir), 0o755); err != nil {
		repo.failure = fmt.Sprintf("cannot create %s: %v; make the home folder writable", display(opts.Home, filepath.Dir(repo.dir)), err)
		return repo
	}
	var output bytes.Buffer
	repo.verb = verb
	syncing := counting(opts.Progress, doing, 1)
	err := opts.Shell(ctx, command, &output)
	syncing.finished()
	if err != nil {
		reason := fmt.Sprintf("`%s` failed: %v%s; if the repo is private, check `gh auth status`", command, err, lastLine(output.String()))
		if verb == "cloned" {
			repo.failure = reason
			return repo
		}
		repo.verb = "not pulled, using the copy from the last run: " + reason
	}
	// A repo can hold a symlink that points outside the clone, skills/
	// itself included. Everything is read through a root opened on the
	// clone, which refuses those, so nothing outside the repo is ever
	// copied into a harness.
	clone, err := os.OpenRoot(repo.dir)
	if err != nil {
		repo.failure = fmt.Sprintf("cannot open %s: %v; make it readable, or delete the clone and rerun", display(opts.Home, repo.dir), err)
		return repo
	}
	repo.roots = append(repo.roots, clone)
	skillsRoot, err := clone.OpenRoot("skills")
	if errors.Is(err, fs.ErrNotExist) {
		repo.failure = "it has no skills/ folder; add one with a folder per skill, each with a SKILL.md, and push"
		return repo
	}
	if err != nil {
		repo.failure = fmt.Sprintf("cannot open its skills/ folder: %v; it has to be a folder inside the repo, not a symlink out of it", err)
		return repo
	}
	repo.roots = append(repo.roots, skillsRoot)
	repo.source = skills.Source{Name: name, Files: skillsRoot.FS()}
	found, err := skills.Names(repo.source)
	if err != nil {
		repo.failure = err.Error()
		return repo
	}
	repo.count = len(found)
	return repo
}

// lastLine is the last thing gh or git printed, the line that says why.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return ""
	}
	return ", " + last
}

// pullLine fast-forwards the clone. Run inside a clone, `gh repo sync`
// fetches from GitHub with gh's login and fast-forwards the local branch,
// which is what the plan reads; --source names the repo itself, since
// without it gh syncs a fork from its parent instead. gh has no flag for
// the folder to work in, so the line runs inside the clone.
func pullLine(operatingSystem, name, dir string) string {
	return inRepo(operatingSystem, dir, "gh repo sync --source "+name)
}

// quote makes a path one argument for the shell setup runs lines in: sh on
// macOS and Linux, PowerShell on Windows. Single quotes in both, with the
// quote character escaped the way each shell wants.
func quote(operatingSystem, path string) string {
	if operatingSystem == "windows" {
		return "'" + strings.ReplaceAll(path, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// buildCatalog lines the sources up, squirrel first. Two kinds of folder are
// left out of every repo. One a tool installs itself, roast or bgr say: a
// repo built from a whole skills folder carries those along, and the tool's
// own install keeps them matched to its binary. And one whose name isn't
// lowercase: on the case-insensitive disks macOS and Windows ship with,
// Voice and voice would land in one folder, and the plan can't tell.
func buildCatalog(embedded assets, repos []skillRepo) catalog {
	toolFolders := map[string]bool{}
	for _, tool := range embedded.tools {
		if tool.SkillFolder != "" {
			toolFolders[tool.SkillFolder] = true
		}
	}
	sources := []skills.Source{{Name: "squirrel", Files: embedded.skills}}
	for index := range repos {
		repo := &repos[index]
		if !repo.usable() {
			continue
		}
		names, err := skills.Names(repo.source)
		if err != nil {
			repo.failure = err.Error()
			continue
		}
		hidden := map[string]bool{}
		for _, name := range names {
			switch {
			case toolFolders[name]:
				repo.toolSkills = append(repo.toolSkills, name)
			case name != strings.ToLower(name):
				repo.oddNames = append(repo.oddNames, name)
			default:
				continue
			}
			hidden[name] = true
		}
		if len(hidden) > 0 {
			repo.source.Files = withoutFolders{FS: repo.source.Files, hidden: hidden}
			repo.count -= len(hidden)
		}
		sources = append(sources, repo.source)
	}
	return catalog{repos: repos, sources: sources}
}

// withoutFolders is a skills folder with some top-level folders hidden.
type withoutFolders struct {
	fs.FS
	hidden map[string]bool
}

func (w withoutFolders) Open(name string) (fs.File, error) {
	if w.hidden[strings.SplitN(name, "/", 2)[0]] {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return w.FS.Open(name)
}

func (w withoutFolders) ReadDir(name string) ([]fs.DirEntry, error) {
	if w.hidden[strings.SplitN(name, "/", 2)[0]] {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := fs.ReadDir(w.FS, name)
	if name != "." || err != nil {
		return entries, err
	}
	kept := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !w.hidden[entry.Name()] {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

func printRepos(out io.Writer, home string, repos []skillRepo) {
	if len(repos) == 0 {
		return
	}
	fmt.Fprintln(out, "\nskill repos")
	for _, repo := range repos {
		if !repo.usable() {
			fmt.Fprintf(out, "  %s  %s, FAILED: %s; setup carries on without it\n", repo.name, display(home, repo.dir), repo.failure)
			continue
		}
		fmt.Fprintf(out, "  %s  %s, %s, %d skills\n", repo.name, display(home, repo.dir), repo.verb, repo.count)
		for _, folder := range repo.toolSkills {
			fmt.Fprintf(out, "  %s  installed by the %s tool itself, the copy in %s is left out\n", folder, folder, repo.name)
		}
		for _, folder := range repo.oddNames {
			fmt.Fprintf(out, "  %s  not a lowercase name, the copy in %s is left out; rename the folder\n", folder, repo.name)
		}
	}
}

// holdBack keeps a skill as installed when the saved pick for it names a
// repo setup has no copy of this run: hidden from every source, the name is
// local for the run, so a lost clone plus a dead network never hands the
// skill back to squirrel. The pick survives in the config the same way.
func holdBack(skillSources catalog, saved map[string]string) catalog {
	unreachable := map[string]bool{}
	for _, repo := range skillSources.unreachable() {
		unreachable[repo.name] = true
	}
	hidden := map[string]bool{}
	for name, source := range saved {
		if unreachable[source] {
			hidden[name] = true
			skillSources.held = append(skillSources.held, name)
		}
	}
	if len(hidden) == 0 {
		return skillSources
	}
	sort.Strings(skillSources.held)
	for index := range skillSources.sources {
		skillSources.sources[index].Files = withoutFolders{FS: skillSources.sources[index].Files, hidden: hidden}
	}
	return skillSources
}

func printHeld(out io.Writer, skillSources catalog, saved map[string]string) {
	for _, name := range skillSources.held {
		fmt.Fprintf(out, "  %s  left as it is in each harness, %s couldn't be reached this run; a harness without it gets it once the repo is reached\n", name, saved[name])
	}
}

// rememberOverrides is what the config keeps: this run's picks, one per
// skill that collides today, over the saved ones. A saved pick whose name
// no longer collides is dropped, but only on a run that reached every repo:
// with one unreachable there is no telling which collisions are gone and
// which are only out of sight, so every saved pick stays.
func rememberOverrides(saved map[string]string, unreachable []skillRepo, picks map[string]string) map[string]string {
	kept := map[string]string{}
	if len(unreachable) > 0 {
		for name, source := range saved {
			kept[name] = source
		}
	}
	for name, source := range picks {
		kept[name] = source
	}
	return kept
}

// unreachable lists the repos setup has no copy of this run.
func (c catalog) unreachable() []skillRepo {
	var repos []skillRepo
	for _, repo := range c.repos {
		if !repo.usable() {
			repos = append(repos, repo)
		}
	}
	return repos
}

// printOverrides says where each colliding skill name comes from on every
// run, so a repo copy that isn't installed never goes unnoticed.
func printOverrides(out io.Writer, collisions []skills.Collision, picks map[string]string) {
	for _, collision := range collisions {
		pick := picks[collision.Name]
		verb := "overridden by"
		if pick == "squirrel" {
			verb = "kept from"
		}
		fmt.Fprintf(out, "  %s  %s %s, not installed from %s\n", collision.Name, verb, pick, strings.Join(without(collision.Sources, pick), ", "))
	}
}

// settleCollisions picks a source for every name more than one source
// holds from a --override flag first, then the saved pick, and returns the
// names neither settles, for the guided flow to ask about. Without a
// terminal, the refusal for the first open one names the flag that picks.
func settleCollisions(collisions []skills.Collision, saved, flags map[string]string) (picks map[string]string, open []skills.Collision, err error) {
	picks = map[string]string{}
	byName := map[string]skills.Collision{}
	for _, collision := range collisions {
		byName[collision.Name] = collision
	}
	for name, source := range flags {
		collision, ok := byName[name]
		if !ok {
			return nil, nil, fmt.Errorf("[SQUIRREL-OVERRIDE] skill %q is not in more than one source, so there is nothing to override; %s", name, collisionList(collisions))
		}
		if !holds(collision, source) {
			return nil, nil, fmt.Errorf("[SQUIRREL-OVERRIDE] skill %q is not in %s; it is in %s; example: --override %s=%s", name, source, strings.Join(collision.Sources, " and "), name, collision.Sources[1])
		}
		picks[name] = source
	}
	for _, collision := range collisions {
		if _, done := picks[collision.Name]; done {
			continue
		}
		if source, ok := saved[collision.Name]; ok && holds(collision, source) {
			picks[collision.Name] = source
			continue
		}
		open = append(open, collision)
	}
	return picks, open, nil
}

func collisionList(collisions []skills.Collision) string {
	if len(collisions) == 0 {
		return "no skill is in more than one source"
	}
	parts := make([]string, 0, len(collisions))
	for _, collision := range collisions {
		parts = append(parts, collision.Name+" ("+strings.Join(collision.Sources, ", ")+")")
	}
	return "the skills in more than one source: " + strings.Join(parts, "; ")
}

func refusal(collision skills.Collision) error {
	flags := make([]string, 0, len(collision.Sources))
	for _, source := range collision.Sources {
		flags = append(flags, fmt.Sprintf("--override %s=%s to %s", collision.Name, source, useWording(source)))
	}
	return fmt.Errorf("[SQUIRREL-SKILL-COLLISION] skill %q is in %s, and there is no terminal to ask which one goes into the harnesses; rerun with %s, or rename the folder in %s", collision.Name, strings.Join(collision.Sources, " and "), strings.Join(flags, ", "), strings.Join(without(collision.Sources, "squirrel"), " or "))
}

// renameStop is the error for the pick that leaves the collision to the
// person: setup stops before touching a harness.
func renameStop(collision skills.Collision) error {
	return fmt.Errorf("[SQUIRREL-SKILL-COLLISION] setup stopped so you can rename the %q folder in %s; push, then rerun squirrel setup. The harnesses are unchanged", collision.Name, strings.Join(without(collision.Sources, "squirrel"), " or "))
}

// UseWording is how the collision screen names each choice: keep squirrel's,
// or use the repo's.
func UseWording(source string) string {
	return useWording(source)
}

func useWording(source string) string {
	if source == "squirrel" {
		return "keep squirrel's"
	}
	return "use " + source + "'s"
}

func holds(collision skills.Collision, source string) bool {
	for _, candidate := range collision.Sources {
		if candidate == source {
			return true
		}
	}
	return false
}

func without(sources []string, skip string) []string {
	rest := make([]string, 0, len(sources))
	for _, source := range sources {
		if source != skip {
			rest = append(rest, source)
		}
	}
	return rest
}
