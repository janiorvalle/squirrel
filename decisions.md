# Decisions

Things we've decided that don't have a file to live in yet.

## For the mode

Written 2026-09-02, while drafting the principle skills. The mode gets written last.

- **Index every time, full file on demand.** One line per principle, read at the start of every multi-step task; the full file only when it applies.
- **Each principle file stands on its own.** Read one at a time, so small overlaps are fine; no cross-links that assume another was read.
- **Mention a principle only when it changed a decision.** Say what it changed. No list at the end of a reply; zero on a small task is fine.
- **Generate the index from the description lines.** One script, one place for the wording.

## Skills are harness-agnostic

Written 2026-09-02, while drafting the how skill.

Every skill works in Claude Code, Codex, Cursor, Pi, or anything else: no harness-specific tool names, agent types, model slugs, or config paths. Say "spawn read-only subagents", "search the code", "use the browser tooling your harness gives you". Shell commands like grep are fine; every harness has a shell.

## The skill index lives in the letter, not the mode

Written 2026-09-02, after the letter became the always-on copy.

The letter is in context on every turn in every harness, so it holds the index that started in the mode. Principles are hand-written sections. Workflows are a table `scripts/build-index.py` generates from each skill's description. The mode carries no copy and doesn't say to read one.

## Third-party skills are committed, not fetched

Written 2026-09-03, while vendoring agent-browser and typescript-best-practices.

A skill lives here when squirrel doesn't control the tool that owns it; fetching at setup time landed unread changes on every machine. Our own tools (quest, roast, bgr, tokenomnom) ship their skill with the binary; we review those repos already.

- `vendor.json` is the pin record.
- The copy is verbatim, license alongside, never edited: the weekly vendor-bump workflow recopies from upstream and opens a PR when it differs, so a hand edit gets a PR putting it back.
- A skill's version is the last upstream commit that touched its folder, so unrelated commits open no PR.
- The text is upstream's voice: `verify.py` only checks that SKILL.md exists, and `build-index.py` leaves vendored skills out of the letter's table.
- `tools.md` still names agent-browser, since setup installs the tool. Its skill is a stub that runs `agent-browser skills get core`; the instructions ship in the CLI at npm's version, and the reviewed PR covers only the stub.
- Bump PRs use the workflow's token, which starts no CI, so the reviewer runs `make verify` before merging. A folder can't fail it anyway, short of losing its SKILL.md.

## Upstream principle names stay dangling, our own skills answer to them

Written 2026-09-03, while deciding whether to vendor the two skills typescript-best-practices names.

`typescript-best-practices` says "apply the type-system-discipline principle skill first" and later points at boundary-discipline. Upstream in `cursor/plugins` those are `pstack/skills/principle-type-system-discipline` and `pstack/skills/principle-boundary-discipline`. Neither is vendored here or will be.

squirrel has both: `strict-types` is type-system-discipline in our voice plus never-any and no-null from the letter, `validate-at-the-edges` is boundary-discipline plus the error contract. Vendoring the originals would put two copies of every rule in every harness, drifting on the first edit, and type-system-discipline points at encode-lessons-in-structure (ours is `dont-say-it-twice`), so each brings the next dangling name.

The rule: the squirrel skill carries the upstream name in its description line, which every harness lists, plus a body note for an agent arriving by search. The reference stays as upstream wrote it, so the weekly bump keeps working. `strict-types` answers to type-system-discipline, `validate-at-the-edges` to boundary-discipline. A shim folder named after the upstream skill was rejected as clutter in every list.

Vendor an upstream principle only when squirrel has no skill for it and doesn't want to write one.

## Setup is a binary with the skills inside

Written 2026-09-03, while replacing setup.py.

setup.py needed a clone and a python path, and `/squirrel-setup` was a silent no-op once installed, looking for the checkout relative to itself and finding `~/.claude`. `squirrel` is a Go binary shaped like roast: one curl line, checksum verified, self-upgrade from GitHub releases. The skills, the letter, `tools.md`, and `vendor.json` are embedded at build time, so setup runs from anywhere, and `go run ./cmd/squirrel setup` from a checkout installs that checkout's files. No repo lookup, no environment variable.

- Harnesses are detected by folder. Claude Code and Codex first check `CLAUDE_CONFIG_DIR` and `CODEX_HOME`; set and non-empty, everything goes under that folder and the plan prints it with the variable beside it. setup.py honored `CLAUDE_HOME`, which Claude Code's docs never mention. OpenCode, Cursor, and Pi get no variable until their docs name one.
- A numbered pick, found harnesses preselected, saved in `~/.squirrel/config.json` so reruns and `squirrel upgrade` don't ask again.
- Backups of overwritten skills and replaced instructions files go to `~/.squirrel/backup/<stamp>/<harness>/`, not `.squirrel-backup` folders and `.bak` files in place. Copies, not renames: `CODEX_HOME` or `CLAUDE_CONFIG_DIR` on another mount fails a rename with EXDEV. A changed skill is retired inside its own folder until the new one is in place.
- The git hooks step is gone; `make install-hooks` in the checkout does that.
- Releases were macOS and Linux at first; the installer and every `tools.md` line were POSIX shell. Windows is below.

Five harness rows. Claude Code and Codex were verified on a real install. OpenCode's `~/.config/opencode/skills/` is per its docs, plural, correcting the second-hand `skill`. Pi's `~/.pi/agent/skills/` and `~/.pi/agent/AGENTS.md` match its docs. Cursor's `~/.cursor/skills` matches its docs, but `~/.cursor/rules/squirrel.mdc` carries over from setup.py and Cursor documents only project-level `.cursor/rules`, so that row is unverified. OpenCode, Cursor, and Pi also read `~/.claude/skills` or `~/.agents/skills`; OpenCode 1.18 lists a skill in both once, by name. Nothing to handle until a harness shows one twice.

## Setup knows outdated, not only missing

Written 2026-09-03, for quest 404.

A tool is missing, outdated, or current. The outdated offer has the missing one's shape: y/N per tool with a terminal, `--update-tools` without. The action is the tool's own install line, idempotent and always the newest release; a separate update command was rejected as a second thing to keep right. After an update, setup rereads the version and reruns the skill install even when a copy exists, since the skill ships with the binary.

`tools.md` carries a `Version` line per tool, the command printing the installed version, parsed like `Check`. The latest comes from the GitHub releases API through the section's `Repo` line, or npm when the install line is `npm install -g`, today only agent-browser. Five requests per run, inside GitHub's sixty an hour without a token, concurrent with a five-second timeout, so a dead network costs one wait. A failed lookup shows "latest unknown" and setup carries on; the network never blocks skills. git and gh have no `Version` line; the system package manager updates them.

Comparison is `golang.org/x/mod/semver`, already a dependency of `squirrel upgrade`, after adding the leading v the tools leave off.

## git and gh are prerequisites, checked but never installed

Written 2026-09-03, for quest 410.

`brew install git gh` in `tools.md` doesn't exist on Linux. An install line per OS by `runtime.GOOS` was rejected: git and gh are what the flow stands on, `gh auth login` is a conversation setup can't have for you, and on Linux the command depends on the distro, apt or dnf or pacman, each wanting sudo unasked.

The rule: a `tools.md` section with a `Check` line and no `Install` line is a prerequisite. Setup checks it, reports it missing with a link to its heading in `tools.md` on GitHub, and never offers to run anything for it, terminal or not. The section lists the ways to get it as prose, one line per platform. The curl tools and agent-browser keep their install lines, the same on every OS.

## The agent-browser CLI is pinned

Written 2026-09-03, for quest 408.

The `skills/agent-browser` stub runs `agent-browser skills get core`, so the instructions ship inside the CLI, and unpinned `npm install -g agent-browser` let a new npm release change what agents execute with no PR here. The `tools.md` line is `npm install -g agent-browser@0.36.0`. Setup treats the pin as the latest and offers the line for any other version, behind or ahead, since a machine ahead runs text nobody here read. `scripts/tool-bump.py` runs in the weekly vendor-bump workflow and opens a PR when npm publishes past the pin, npm version and upstream release linked. A human merges.

The pin lives in the install line, not `vendor.json`, since that's what setup runs and a person reads. The bump is its own script, not a new entry kind in `vendor-bump.py`; they share only the PR ceremony, which is in the workflow. The vendored folder and the pinned CLI move independently, each through its own PR, in either order.

## Windows: the same binary, a PowerShell line beside each shell line

Written 2026-09-03, for quest 414.

The release builds for Windows, zipped like roast's, with `install.ps1` copied from quest's: download the zip, verify the SHA-256 against `checksums.txt`, smoke the binary, put `squirrel.exe` in `%LOCALAPPDATA%\Programs\squirrel`, add that folder to the user PATH, run `squirrel setup`. Self-upgrade already had roast's Windows dance: backup beside the binary, hidden cleanup command waiting for the old process to exit.

Each `tools.md` line runs in the OS's shell, `sh -c` or `powershell -NoProfile -Command`, picked by `runtime.GOOS` in one function. A plain line is the default; one suffixed with Go's OS name, `- Check (windows): \`Get-Command quest\``, overrides it there. The parser resolves the suffix, so `Tool` and setup see one line. Only `Check` and `Install` need a variant; `Version` and the skill lines carry no suffix. One line for both shells was rejected as unreadable.

Only quest shipped a PowerShell installer then, so only quest got a runnable `Install (windows)` line. roast, bgr, tokenomnom, and TruffleHog had Windows archives with no installer, and download-and-unzip with no checksum is the unreadable trick again. Their line was a sentence with no backticks, which the parser treats as a step for a person: setup shows it, never runs it, like a prerequisite. Once a repo ships an `install.ps1` the line becomes `irm ... | iex` and setup installs it. Until then Windows got the skills, the letter, and quest from setup, roast and bgr by hand.

The harness rows resolve unchanged: `os.UserHomeDir` is `%USERPROFILE%`, Claude Code's docs say `~/.claude` means `%USERPROFILE%\.claude` and `CLAUDE_CONFIG_DIR` moves it, and Codex's home-dir crate reads `CODEX_HOME` when set and non-empty, else `~/.codex` through `dirs::home_dir`. OpenCode, Cursor, and Pi stay unverified on Windows. An instructions file saved with CRLF is written back with LF, so the block matches next run.

CI on `windows-latest` runs the Windows-tagged tests, then `scripts/install-smoke.ps1`: build and zip, run `install.ps1` twice, refuse a bad checksum, run setup into a throwaway profile where the `Get-Command` check for git and gh passes through PowerShell.

## Setup puts ~/.local/bin on PATH for the lines it runs

Written 2026-09-03, for quest 416.

Every `tools.md` line runs with `~/.local/bin` first on PATH when it isn't there yet. Every curl installer puts its tool there, a fresh machine has the folder off PATH until the shell profile says so, and the check after an install failed without saying why. The prefix lives in `shellArguments`, next to the Windows line that refreshes the user PATH from the registry, so the Go side knows about neither. A PATH that already has the folder keeps its order, so an earlier tool still wins. A check that fails after an install names PATH and the folder the installer printed.

Rejected: an error naming `~/.local/bin` that leaves the run failing, since setup put the tool there; and PATH only for the post-install check, since the next run would reinstall and the skill line needs it too.

The person's terminal still can't see the folder, so every run where `~/.local/bin` exists off PATH says so, with the profile line to add; `install.sh` prints the same line.

## Windows installs the tools from setup

Written 2026-09-03, for quest 417.

roast, better-git-review, and tokenomnom each carry an `install.ps1` in their repo, squirrel's with names swapped: download the release zip for the CPU, verify the SHA-256 against `checksums.txt`, run `--version` and require the release's, put the executables in `%LOCALAPPDATA%\Programs\<tool>`, add that folder to the user PATH. bgr and tokenomnom install both executables. Each repo's Windows CI builds and zips them, runs the installer twice, refuses a bad checksum. Their `Install (windows)` lines are `irm ... | iex`, like quest's, so setup runs them.

TruffleHog ships no Windows installer; its asset is a tar.gz beside a versioned checksums file. winget was rejected: `microsoft/winget-pkgs` has no TruffleHog manifest, only Trufflesuite under `manifests/t`. scoop was rejected as a second package manager on a bucket squirrel doesn't control. So `scripts/install-trufflehog.ps1` is the same installer with tar.gz for zip, `tar -xzf` for `Expand-Archive` (Windows 10 ships bsdtar), and `trufflehog_<version>_checksums.txt` for `checksums.txt`. It's embedded in the binary; setup writes it to `~/.squirrel/scripts` before any tool line, and the Windows line reads it into `iex`. Its raw URL on main was rejected: it exists only after the merge, so no branch could pass CI, and a released binary would run whatever main had that day; embedding needs no network. Checksum verification is in all five lines.

Every installer sends a GitHub token on the latest-release lookup when `<TOOL>_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN` is set, in that order, quest's; GitHub allows sixty requests an hour per address, CI runners share one, and the 403 read as no release found. The message carries the HTTP status and, on 403 or 429, names the variables and the version override that skips the lookup.

An `Install` line with no backticks is still a step for a person; no tool uses the shape now, and the rule stays.

With the PATH fix from quest 416, `setup --install-tools` on a fresh Windows box installs and passes the `Get-Command` checks in one run, which the CI smoke checks.

## Your own skills come from a repo you name

Written 2026-09-04, for quest 418.

Setup asks once for a GitHub repo, `owner/name`, clones it into `~/.squirrel/repos/<owner>/<name>`, fast-forwards it every run, and installs its `skills/` through the same plan as squirrel's own. Clone and sync are `gh repo clone` and `gh repo sync --source owner/name`, in the shell the `tools.md` lines use, so gh's login reaches GitHub and the binary never sees a token; plain git has no credentials for a private clone until `gh auth setup-git`, and `--source` keeps a fork syncing from itself. Only `skills/` is read; a second letter is the drift setup exists to prevent.

One list of sources, squirrel's embedded folder then each repo, so a cloned `skills/` is a filesystem the same code plans and applies. Every planned skill carries its source, which names the repo in the report and keeps "local, untouched" true for anything no source owns. One plan per source, merged, was rejected: each would list the others' skills as local.

The same name in two sources is a collision, and setup stops. A terminal asks: keep squirrel's, use the repo's, or rename. Without one it refuses and prints the `--override name=source` line that answers. The pick is saved per skill in `~/.squirrel/config.json`, printed every later run, and dropped only when a run reached every repo and found no collision. The repo question is asked once and remembered even when skipped, as an explicit `skill_repos_asked`.

Rules that keep a repo from doing harm:

- A failed sync keeps last run's copy. An unreachable repo never hands its skills back to squirrel.
- The clone is read through `os.OpenRoot`; a symlink pointing outside it, `skills/` itself included, is refused.
- A folder a tool installs itself (`quest`, `roast`, `bgr`, `tokenomnom`) is left out; the tool keeps it matched to its binary.
- A folder that isn't a lowercase name is left out, and a local skill differing only in case stops the plan: `Voice` and `voice` are one folder on the disks macOS and Windows ship with.
- A file the harness copy has and the source lacks is drift, hidden or not, except the few files the desktop drops into folders it browses; a file a repo deletes leaves the harness next run.
- Forgetting a repo leaves its clone and installed skills alone, except a skill that shadowed a squirrel skill, which goes back to squirrel's copy with a backup.

## An install line downloads first, so a failed download fails the line

Written 2026-09-04, for quest 422.

Every curl line in `tools.md` was `curl -fsSL <url> | sh`. On a failed download sh reads nothing and exits zero, a pipeline's status is the last command's, so setup printed installed and the check afterward blamed PATH. Now each line is `script=$(mktemp) && curl -fsSL -o "$script" <url> && sh "$script"`, stopping with curl's error. TruffleHog's line passes `-b ~/.local/bin` to the file instead of through `sh -s --`. The Windows lines stay `irm ... | iex`; a failed `irm` throws before `iex`. `set -o pipefail` was rejected: Linux `sh` is dash, which has none. The temp file is left for the OS; a trailing `rm` would hide the exit status or add a step to every line for a few kilobytes in `$TMPDIR`. A test reads the real `tools.md` and fails any curl line that pipes into sh.

## Tool skills reach every picked harness

Written 2026-09-04, for quest 424.

quest, roast, bgr, and tokenomnom install their skill into Claude Code's and Codex's folders, so OpenCode and Pi never got them, and every run reran every skill install. Now, after a tool's skill install line, setup copies the folder the tool wrote into each picked harness that lacks it.

- The source is whichever of `~/.agents/skills`, Claude Code's, and Codex's folders the tool wrote last, by the time on its `SKILL.md`. On a fresh machine that's Claude Code's; no tool writes to `~/.agents/skills` today, and an older folder there never wins over what the install just wrote.
- A folder setup copied into is never the source, so a hand edit in Pi is replaced next run, not mirrored.
- A folder in `~/.agents/skills` no longer counts as present for every harness. Claude Code and Codex don't read it, and an older copy there hid the newer one. Every picked harness gets its own copy.
- The tool's own install is the source of truth. No source owns the copy, so the skills plan reports it local and never touches it.
- Present means every picked harness has it as the tool wrote it. A copy an update left behind, a failed copy, or one whose source folders were deleted reruns the tool's install and is replaced, the old copy backed up like any changed skill in the run's backup folder.

## The tracker is a line in the repo, and one skill speaks every backend

Written 2026-09-04, for quest 419.

Quest was the tracker, named in the letter, the mode, land-pr, prove-it, tools.md, and setup. The human stopped using it, and agents' tickets ran to ten paragraphs for a button move. Quest leaves every file and the tool list, so setup stops installing it. A repo names its tracker on one line of its instructions file: `Tracker: linear SR`, `Tracker: markdown tasks/`, `Tracker: github-issues`, `Tracker: jira SR`. One word for the backend, one for what it needs, greppable, read by setup and the mode alike. squirrel's own is markdown tasks under `tasks/`. This repo's `AGENTS.md` is also the letter, so the installer leaves any `Tracker:` line out of the block it writes.

One skill, `tracker`, not one per backend. The contract is the same everywhere: claim before touching files, record the files, turn in with the PR and the evidence, a human completes after the merge, evidence on the ticket. Those five verbs and the ticket shape are the whole skill; a backend is fifteen lines at the bottom mapping verbs to commands. Four skills would have drifted four ways and loaded the wrong one.

`scripts/ticket-lint.py` enforces the shape: Problem in two sentences from the person who hits it, Fix in two or three, Done when in up to four observable lines, Out of scope optional, 120 words in all. Prose asking for short tickets left them walls, so the agent runs the check before filing. Files to touch and design notes go in the PR.

Evidence stays on the ticket: comments with links and attachments for Linear, Jira, and GitHub Issues. Markdown tasks live in git and evidence never enters git, so the ticket file gets the PR link and the PR holds the files, screenshots as comment attachments and the walkthrough as a gist. An MCP is per harness and machine, so the Linear and Jira sections say "connect the MCP in your harness" and nothing more; `scripts/verify.py` refuses harness names in skills.

Added 2026-09-04, for quest 420: a repo with no `Tracker:` line no longer stops the agent. It asks which backend in the Decide shape, writes the answer into the instructions file on branch `tracker-line` in its own worktree, offers the one-line PR, then claims.

## Setup asks where the repos live and which tracker each one uses

Written 2026-09-04, for quest 421.

Setup asks once where the repos live, guessing folders under home with git checkouts one level down (`code`, `github`, `src`, `projects`, `dev`), saved as `repos_dirs` and `repos_dirs_asked` in `~/.squirrel/config.json`; `--repos-dir <folder>` answers without a terminal. The plan lists every checkout with its `Tracker:` line or `not declared`, from `AGENTS.md` then `CLAUDE.md`, one listing per folder and two reads per checkout, no git, no network.

The tracker question repeats every run per undeclared repo, skip per run, since the answer lives in the repo. A terminal gets a numbered pick of the four backends plus skip, then what the backend needs, then whether the same answer covers the rest. The line goes after the opening heading, or first, into `AGENTS.md`, or `CLAUDE.md` when that's the only file, or a new `AGENTS.md`. Without a terminal setup writes nothing; someone's repo is their code.

The PR offer follows the write on a clean tree with an origin: branch `tracker-line`, commit `docs: name the tracker`, `gh pr create` with a body in the ticket shape, then back to the original branch even if a step failed. A dirty tree gets the line and no offer. Until the PR merges the scan sees the `tracker-line` branch, reports waiting, and doesn't ask; it's read from the common git dir (`.git` file and `commondir` of a linked worktree), loose ref or `packed-refs`, so gc doesn't reask. An `AGENTS.md` linking to `CLAUDE.md` is followed one hop and the tracked file written and staged; a link outside the repo is reported and left, and the containment check follows chains all the way.

Before the write, so the line never counts as pending:

- Default branch, per `origin/HEAD`. None (`git init` plus `git remote add`) means no offer and the `git remote set-head origin -a` line, not a guess of main or master.
- HEAD at `origin/<default>`, else push or pull first.
- `git remote get-url origin`, not `gh repo view`, which is one API request per repo before any yes.

After the yes, before any branch, `gh repo view` on the origin push URL, the remote the push goes to, must succeed, so GitLab or Bitbucket never gets a pushed branch with no PR. That is the one API request. If `gh pr create` still fails, the branch keeps the line and gh's message is reported. The push runs `git -c credential.helper='!gh auth git-credential'`, since a fresh machine has gh's login and no git credential helper, and `gh auth setup-git` would change the person's git config for good. A failure before the commit restores the checkout, deletes the empty `tracker-line` branch, and reports the line written and uncommitted. All git and gh go through the tools shell in the checkout, so the binary holds no token.

Questions and writes run after the tools, so an early stop touches no repo. A failed repo step is reported at the end like a tool step. q at a tracker question stops asking and leaves the rest, without the "nothing changed" message, since the harnesses have changed.

## Setup is a guided flow on the terminal, and an update goes through the binary's owner

Written 2026-09-05, for issue 42.

The line prompts confused: arrow keys spilled escape codes, saved picks hid a harness added later, and a Homebrew TruffleHog got an install line that put a second copy in `~/.local/bin` behind brew's on PATH. With a terminal, setup is now one screen per question on charm's huh over bubbletea, which tokenomnom already ships. Harnesses every run, saved picks and any harness found since the last run checked, one left unchecked on purpose staying so, which takes `harnesses_found` in the config, what the screen offered last time. The skills repo and the repos folder once. Per undeclared repo, the backend, what it needs, the PR, and same for the rest. The tools as a checkbox list. Esc goes back, Ctrl-C quits with nothing changed, the plan prints last with a confirm, and nothing in a harness or a repo of yours is written before it. The skills repo clone under `~/.squirrel/repos` is still synced when named, as before, because the collisions it can raise are the next question. Nothing to apply means no confirm, so a rerun with nothing changed is one Enter.

- One screen is one huh field in its own bubbletea program, and an answered screen collapses to one line. A single program with a state machine was rejected: shell lines run between screens on the terminal's stdin, and a program holding the tty while `gh repo clone` runs is a fight over keystrokes.
- The screens only collect answers and render. `internal/setup` gained a `Session` with the facts each screen shows, cached so Esc is free, and `Plan` and `Apply` for the answers. The flag path goes through the same session, so every rule lives once and its tests pass as they were. `cmd/squirrel` picks the path: a terminal without `--yes` gets the screens. `internal/prompt` is gone.
- The PR question moved before the write, since nothing asks during the apply. The repo's state is read through git before the question and again before the write.
- An update line is decided by who owns the binary: `command -v` in setup's shell, links followed. Under node's global `node_modules`, from `npm prefix -g`, it's npm's. A link into `$(brew --prefix)/Cellar/<formula>/` is Homebrew's, and the formula is read off that path, so `tools.md` needs no formula line. A file in `~/.local/bin` is the installer's. Anything else is shown with its path and gets no offer. A file in Homebrew's bin that isn't brew's link is nobody's: this Mac had one, a stray TruffleHog 3.97.0 over an unlinked 3.96.0, which `brew upgrade` would have left standing. The flag path uses the same rule. A `Check` line that names no single binary keeps the install line. A `brew upgrade` that leaves the version behind the release means Homebrew is behind, not PATH.
- Windows: bubbletea runs in Windows Terminal and PowerShell, but the CI smoke uses the flag path, so the screens aren't exercised there. The owner rule reads `(Get-Command <binary> -ErrorAction SilentlyContinue).Source`, treats `%LOCALAPPDATA%\Programs` as the installer's folder and the npm prefix as npm's, where the .cmd shims sit; untested on Windows.

## The project is called squirrel

Written 2026-09-05.

The binary was called jstack, and so is the JDK's thread-dump tool. Every machine with Java has that one first on PATH, and macOS ships a `/usr/bin/jstack` stub even without Java, so typing the name ran the wrong program and answered "could not find any processes matching: upgrade". A tool that silently runs something else when you type its name is the opposite of easy, so the name changed rather than the PATH advice.

squirrel, because the stack is an attention aid for agents: one task, no skipped steps, prove it before saying done. The squirrel is the distractible animal and this one stays on task; the stash in the hero image is the stack. The repo, module, binary, installers, config folder (`~/.squirrel`), letter markers, and the two skills (`squirrel` for the mode, `squirrel-setup` for setup) all renamed in one pass. No migration code: nobody outside the author had installed it, so the old state on each machine is deleted by hand and the new one installed fresh. That includes the worktree lock registry, which moved from `~/.config/jstack` to `~/.config/squirrel`; a session started before the rename keeps its locks in the old file, so finish or release those worktrees before deleting it. The data-outlives-code rule was set aside here on purpose, once, by the human.

## The tracker step is one list per backend, and skips are remembered

Written 2026-09-05, for issue 47.

With a hundred checkouts under `~/code` the tracker step was a hundred screens, each followed by "same for the rest?" and a PR question, with the remaining repos printed under every one. The person hit Ctrl-C after twenty and lost everything. Now it is one checkbox list per backend, Linear, Jira, GitHub Issues, then markdown tasks, over the undeclared repos no earlier list took, `/` to filter by name, the key or folder typed once per list, and a list with nothing left to offer isn't shown. Unchecked on every list is skip, saved as `trackers_skipped` in the config by canonical checkout path, so the repo isn't offered again; `--ask-trackers-again` clears the list for the run, and a repo that names its tracker drops off it. The answered list collapses to one line, the backend and a count.

- The guess is the origin's and nothing else: `gh repo view --json hasIssuesEnabled` on the push URL, once per origin per run, and a repo gh can see with issues on comes checked on the GitHub Issues list. This is the gh call per repo before the yes that the earlier decision avoided; the human asked for the guess, and the call also tells the plan which repos gh can't open a PR for. Any failure reads as an origin gh can't see: no guess, line only.
- The PR question is one confirm after the plan, "open N pull requests", since the plan is where each repo's reason lives: the line and a PR, or the line only because the tree is dirty, there is no origin, no `origin/HEAD`, a feature branch, local commits ahead, or gh can't see the origin. Repos that can't take a PR still get the line. The apply reads git again before each write, as before.
- The flag path is untouched: no git or gh runs there, nothing is written, and the skip list is kept as it was except that `--ask-trackers-again --yes` clears it.
- A skipped repo is a hold, the same structure as a repo waiting on its `tracker-line` branch, so the plan lists it with its reason and the flag path's count leaves it out.

## The Linear list asks a key per team, and Jira is gone

Written 2026-09-05, for issue 51.

The one list per backend from issue 47 asked one Linear team key for every repo checked. A person whose repos sit in several teams got the wrong key on most of them, and the only way out was one run per team with `--ask-trackers-again`. Now Linear asks in rounds: the key first, "Linear team key, Enter for none", then the checkbox list of the repos in that team over the repos no earlier screen took, then the key again, empty, until Enter leaves it empty or the last repo is taken. Esc walks the rounds back with the keys and checks kept. Each repo gets the key of the list it was checked on, which the setup package's per-repo answer already carried, so nothing changed there. The backend leaves one line when it ends, "linear SR  5 repos, linear KC  3 repos", the same key twice counted as one. Jira is gone in the same change, from setup, the tracker skill, and the docs, by the human's call: they use Linear and GitHub Issues, nobody else has installed squirrel, and an unused backend is code and prose to keep right.

- The key comes before the list because the list's title names the team, "Which repos track their work in Linear team SR?". Picking the repos of a named team reads better than picking repos and being asked which team afterwards.
- A single-team person sees the same screens plus one Enter, on the empty key, and not even that when the team took every repo.
- GitHub Issues takes no key and markdown tasks has a default folder, so both stay one list. The rule is the backend's shape: an argument with no default is asked per round.
## The tracker scan runs eight at a time and remembers what gh said

Written 2026-09-05, for issue 50.

With 143 checkouts and 120 origins the screen went blank for over a minute after the harness screen: five git commands per repo and one `gh repo view` per origin, over 800 processes one after another. Now `Session.Trackers` reads every repo through a pool of eight workers, then asks gh about the distinct origins through the same pool, results written back by index so the questions keep their name order. Eight, because a two-core machine must not fork 800 processes, and eight puts a hundred repos under a few seconds. A cancelled context hands out no more work.

What gh said about each origin it could see is saved in the config as `origins`, push URL to issues and when, pruned on apply to the origins the scan met. An origin gh couldn't see is asked again next run, since a failure isn't a fact. The push URL loses what stands before the host, over http a token. `--ask-trackers-again` clears the map along with the skips. The flag path runs no git or gh and keeps the map. Issue 49's live count wraps the per-index work in `forEach`.

## Setup says what it's doing while the tracker scan runs

Written 2026-09-05, for issue 49.

The scan from issue 50 still left the terminal blank for seconds, a minute on a slow disk or network, so it looked hung. Now `Options.Progress` takes each step of a wait as it moves, and the terminal draws one line in place, "reading repos 37 of 143, asking gh about origins 12 of 120", once the wait has run a second, so a three-repo scan flickers nothing. The flag path passes no callback and prints nothing new. The skills repo pull and the tools step got the same line, since either runs over a second on a slow network. Whatever comes next takes the line off: a screen, or any line setup prints, since setup prints through the writer that holds it. A pty that reports no size gets 80 by 24, where before the list's filter input panicked.
