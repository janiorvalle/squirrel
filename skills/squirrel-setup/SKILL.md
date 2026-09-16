---
name: squirrel-setup
description: "Use to put squirrel on a machine or bring it up to date. Runs the squirrel binary's setup: it finds the coding agents on the machine, shows the plan, asks which harnesses to install into, copies the skills, squirrel's and the ones from a skills repo of your own, puts the letter in each instructions file, and offers the tools the flow needs. Reports what's still missing."
---

# Setup squirrel

`squirrel setup` is the whole thing. The binary carries the skills, the letter, and the tool list, so it runs from any directory with no checkout.

## Get the binary

```sh
curl -fsSL https://raw.githubusercontent.com/janiorvalle/squirrel/main/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/janiorvalle/squirrel/main/install.ps1 | iex
```

The installer verifies the checksum, puts `squirrel` in `~/.local/bin`, or `squirrel.exe` in `%LOCALAPPDATA%\Programs\squirrel` on your user PATH, and runs `squirrel setup` once. `squirrel upgrade` fetches the newest release and reruns setup with the saved picks.

## What setup does, in order

1. **Ask.** With a terminal, one screen per question. Arrow keys and space pick, Enter continues, Esc goes back, Ctrl-C quits with nothing changed. Every run: the harnesses, a checkbox list with your saved picks and any harness found since the last run checked, so a rerun with nothing changed is one Enter and a harness you left out stays out. Once: a skills repo of your own, `owner/name`, or Enter to skip. Once: where your repos live, a folder found under home or one you type, comma separated for more. Then the tracker lists in step 6, one per backend. Then the tools, a checkbox list of the missing and outdated ones with the line each would run; unchecked means skip. Without a terminal, setup prints the plan and the exact rerun line and changes nothing in the harnesses, though a saved skills repo is still synced.
2. **Plan.** A preview of steps 3 to 6 that changes nothing: each skill as new, changed, the same, or local, with the repo it comes from; what happens to each instructions file; each tool as missing with its install line, outdated with both versions and the update line, or current; each repo's `Tracker:` line or `not declared`, and what your answers do to the undeclared ones. Until a repos folder is named, that section lists the folders under home that hold checkouts. With a terminal the plan ends with a confirm. No harness or repo of yours changes before it, though a skills repo clone under `~/.squirrel/repos` is synced as soon as you name it. Nothing to apply means nothing to confirm.
3. **Skills.** Copies the embedded skills and each repo's into each picked harness's skills folder. A skill that differs is moved to `~/.squirrel/backup/<stamp>/<harness>/skills/` first. A skill no source owns is never touched.
4. **Letter.** Puts `AGENTS.md` between `<!-- squirrel:start -->` and `<!-- squirrel:end -->` in each picked harness's instructions file. A file with other content is replaced and backed up beside the skill backups, two letters side by side being the drift this stack exists to prevent, and the plan names any file it would replace. `--keep-instructions` appends the block instead. Once the markers are in, later runs change only the text between them, flag or not. A Cursor `squirrel.mdc` not starting with squirrel's frontmatter is replaced and backed up even with the flag. A `Tracker:` line in the letter stays out of the block: it names one repo's tracker, and every repo reads the instructions file.
5. **Tools.** A missing tool is installed only when checked or with `--install-tools`; an outdated one is updated only when checked or with `--update-tools`. The latest-version lookup is one request per tool, and a failed lookup shows the tool as current with "latest unknown". git and gh have no version or install line: they're missing or present, with a link to their `tools.md` section. An update goes through whoever owns the binary. Setup finds it with `command -v` and follows its links: under node's global `node_modules` for an npm install line, the pinned line; a link into Homebrew's Cellar, `brew upgrade <formula>`, the formula read off the path; a file in `~/.local/bin`, the tool's own install line; anything else, including a file in Homebrew's bin that isn't brew's link, gets its path shown and no offer, because the install line would drop a second copy that loses on PATH. After an update setup reads the version again. Each present tool's skill install runs where its skill is missing, and again after an update, so the skill matches the binary. Tools write their skill into Claude Code's and Codex's folders; setup copies it into a picked OpenCode, Cursor, or Pi that lacks it, from whichever of `~/.agents/skills`, Claude Code's, and Codex's folders the tool wrote last. That copy is the tool's, listed as local and left alone. A copy that no longer matches what the tool wrote counts as missing, so the install runs again and replaces it, old copy backed up.
6. **Repos.** Every git checkout one level down each repos folder, with the tracker read from `AGENTS.md`, then `CLAUDE.md`, from the disk alone. With a terminal, the backends in turn, Linear, GitHub Issues, then markdown tasks, over the undeclared repos no earlier screen took; `/` filters by name on every list. Linear asks in rounds: the team key, Enter with nothing for none, then the checkbox list of the repos in that team, then the key again for another. Each repo gets the key of the list it was checked on. GitHub Issues is one list, and markdown tasks one list with the folder typed once for every repo checked. A repo whose origin gh can see with issues enabled comes checked on the GitHub Issues list, one `gh repo view` per origin, and what gh said about each origin it could see is saved as `origins` in the config so a later run asks only about origins it hasn't met; nothing else is guessed. The git and gh reads run eight at a time, so a hundred repos take seconds. Unchecked on every list means skip, saved as `trackers_skipped` in the config so the repo isn't offered again; `--ask-trackers-again` offers every skipped repo again and asks gh again about every origin, and a repo that names its tracker drops off both lists. The plan then names each repo as getting the line and a PR, or the line only with the reason, and one question, open N pull requests, covers them all. After the confirm the line goes on its own line after the opening heading of `AGENTS.md`, first when there's no heading, into `CLAUDE.md` when that's the only file, or into a new `AGENTS.md` alone when there's neither. A PR is possible when the repo has an origin gh can see, nothing else pending, and its default branch, the one `origin/HEAD` names, checked out at the remote's commit; setup reads that state through git and gh before the lists and again through git before the write. The PR is branch `tracker-line`, commit `docs: name the tracker`, `gh pr create` with the ticket-shape body, then back to the previous branch. Other uncommitted changes, a feature branch, or local commits ahead get the line and no offer. No `origin/HEAD` gets the `git remote set-head origin -a` line to run first. Until the PR merges the repo shows as waiting on `tracker-line` and isn't offered; delete the branch to be offered again. An `AGENTS.md` that links outside the repo is reported and left alone. git and gh run in the tool lines' shell, so gh's login reaches GitHub. Without a terminal nothing is written.
7. **Picks.** Saves the harnesses, skills repos, collision picks, repos folders, skipped repos, and what gh said about each origin to `~/.squirrel/config.json` for later runs. `--harness` replaces the harnesses. `--repos-dir <folder>` adds a repos folder and settles that question. `--ask-trackers-again` clears the skipped repos and the origins for the run.
8. **Report.** Installed, updated, backed up, or left missing, and a reminder to restart the harness.

## Your own skills repo

A GitHub repo, `owner/name`, with a `skills/` folder, one folder per skill, each with a `SKILL.md`. Only `skills/` is read, and there is no second letter. Setup installs the skills beside squirrel's through the same plan. After that:

```sh
squirrel setup --skill-repo owner/name          # add a repo, more than one is fine
squirrel setup --forget-skill-repo owner/name   # stop installing from it
squirrel setup --no-skill-repo                  # there is none; setup stops asking and hinting
```

Forgetting a repo leaves its skills in the harnesses as local skills, untouched, except one that had replaced a squirrel skill, which goes back to squirrel's copy, yours backed up.

The clone in `~/.squirrel/repos/<owner>/<name>` is synced with `gh repo sync` on every run, plan-only included, so a pushed skill shows as changed next time, old copy backed up. Both go through gh, so a private repo needs only `gh auth login`. A repo gh can't reach, or one with no `skills/` folder, is reported with the reason and setup carries on, and `gh auth status` covers the private case. A failed sync keeps the last copy and says so. A file symlinking outside the repo is refused. Left out of a repo and reported: a folder named after a tool's own skill, `roast` or `tokenomnom`, since the tool installs that one and keeps it matched to its binary, and a folder that isn't lowercase, since `Voice` and `voice` are one folder on a Mac. A local skill differing from a source's only in case stops setup with the two names.

The same skill name in squirrel and your repo, or in two of your repos, stops setup. With a terminal it asks on its own screen: keep squirrel's, use yours, or rename it yourself. The pick is saved and printed on every later run as `overridden by owner/name` or `kept from squirrel`. Without a terminal it refuses and prints the flag:

```sh
squirrel setup --harness claude --yes --override land-pr=owner/name   # use yours
squirrel setup --harness claude --yes --override land-pr=squirrel       # keep squirrel's
```

## From an agent

You have no terminal, so `squirrel setup` prints the plan and changes nothing in the harnesses; a saved skills repo is still synced. Read it and bring the decision to the human in the fixed shape:

**Decide:** which harnesses squirrel installs into.
**Options:** the harnesses the plan found, one line each with what changes there.
**Recommendation:** the found ones, unless the plan shows an instructions file with other content, in which case say so and offer `--keep-instructions`.

In the same message, ask about a skills repo while the plan ends with `add --skill-repo owner/name`, and where their repos live while the repos section says `add --repos-dir <folder>` with the folders it found. Pass `--skill-repo` or `--no-skill-repo`, and `--repos-dir`, on the rerun. The tracker lists need a terminal: for repos listed `not declared`, the human runs `squirrel setup` themselves, with `--ask-trackers-again` for the ones listed as skipped, or you write the `Tracker:` line as the `tracker` skill describes.

Then rerun with what they chose:

```sh
squirrel setup --harness claude,codex --yes
squirrel setup --harness all --yes --install-tools
squirrel setup --harness all --yes --install-tools --update-tools
squirrel setup --harness claude --yes --keep-instructions
squirrel setup --harness claude,codex --yes --skill-repo owner/name
squirrel setup --harness claude,codex --yes --repos-dir ~/code
```

Read the report back. A tool still missing or outdated: quote its install line. git or gh missing: setup never installs them, so send the human to their section of `tools.md` for their platform's command, then `gh auth login`, then rerun. A skill refused for being in two sources: a decision with the two `--override` lines it printed as the options, then rerun with the pick. Anything changed: they restart the harness.

## Harnesses

| Key | Harness | Found by | Skills go to | Letter goes to | Moved by |
| --- | --- | --- | --- | --- | --- |
| `claude` | Claude Code | `~/.claude` | `~/.claude/skills` | `~/.claude/CLAUDE.md` | `CLAUDE_CONFIG_DIR` |
| `codex` | Codex | `~/.codex` | `~/.codex/skills` | `~/.codex/AGENTS.md` | `CODEX_HOME` |
| `opencode` | OpenCode | `~/.config/opencode` | `~/.config/opencode/skills` | `~/.config/opencode/AGENTS.md` | |
| `cursor` | Cursor | `~/.cursor` | `~/.cursor/skills` | `~/.cursor/rules/squirrel.mdc`, with `alwaysApply` | |
| `pi` | Pi | `~/.pi/agent` | `~/.pi/agent/skills` | `~/.pi/agent/AGENTS.md` | |

On Windows `~` is `%USERPROFILE%`, where Claude Code and Codex read their folders from, so the rows resolve unchanged. A row's variable, set and non-empty, moves where that harness is found and where its skills and letter land; the plan shows it as `/work/codex (CODEX_HOME)`. An empty variable means the default.

OpenCode, Cursor, and Pi also read skills from Claude Code's folder or the shared `~/.agents/skills`. OpenCode was checked with the same skill in both and lists it once, keyed by name. Cursor and Pi are unverified. `decisions.md` has what was checked.

## Adding a vendored skill

One entry in `vendor.json`: repo, path inside it, pinned commit, license, and where the license file is. The pinned commit is the last upstream commit that touched the folder, not the repo head, or the first bump PR only moves the pin. Then `python3 scripts/vendor-bump.py restore <name>` copies the folder into `skills/` at that commit, license file alongside. The weekly vendor-bump workflow opens a PR whenever upstream moves. Never edit a vendored skill's text; the next bump would overwrite it.

## Adding a tool

One section in `tools.md`. The binary parses the lines, so keep the format. The next release carries the tool.

- `Check` and `Install`, plus `Skill install` and `Skill folder` when the tool ships a skill. A prerequisite setup checks but never installs gets no `Install` line and how to get it in prose; setup reports it missing with a link to the section.
- `Version`, the command printing the installed version, and `Repo`, the GitHub page, when setup should offer updates. The latest comes from that repo's releases, or from the npm registry when the install line is `npm install -g`. Pin it, `npm install -g name@1.2.3`, when the tool carries text agents execute, as agent-browser does: the pin is then the latest, offered to any machine not at it, and the weekly vendor-bump workflow opens a PR when npm moves past it.
- Lines are POSIX shell, run in `sh` on macOS and Linux, unless they carry an OS suffix. The suffixed line wins on that OS, in PowerShell on Windows, and the plain line serves every other OS. `Check (windows)` is `Get-Command name`, since PowerShell has no `command -v`. `Install (windows)`, when the install differs, is a PowerShell command in backticks, `irm https://.../install.ps1 | iex`. Windows lines use `;` between steps and single quotes.
- A tool whose upstream ships no PowerShell installer gets one in `scripts/` here, embedded in the binary. Setup writes it to `~/.squirrel/scripts` before any tool line runs, and the line runs it from there, like TruffleHog's.

## Developing the binary

From a checkout, `make setup` runs `go run ./cmd/squirrel setup` with that checkout's skills embedded. `make verify` is the gate.
