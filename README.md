# squirrel

<p align="center">
  <img src="assets/hero.png" alt="squirrel. chaos outside. a stash inside." width="840">
</p>

How I work with coding agents, written down as skills.

One flow from claiming a task to turning it in with proof. Twenty-four principles, each one rule with a test. Twenty-six workflows for the parts that need steps. A mode skill that ties it together. Every file is written for an agent in Claude Code, Codex, Cursor, or anything else that loads skills from a folder.

The opinions are the point. A human stays in the merge seat. Every claim ships with evidence. The obvious solution beats the clever one. If you don't agree, this isn't your stack, and that's fine.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/janiorvalle/squirrel/main/install.sh | sh
```

That puts the `squirrel` binary in `~/.local/bin`, checksum verified, and runs `squirrel setup`. Setup is a guided flow in the terminal, one screen per question: which coding agents to install into, a skills repo of your own, where your repos live, which repos use each tracker, and which tools to install or update. Arrow keys and space pick, Enter continues, Esc goes back. The last screen is the plan with a confirm, and nothing in a harness or a repo of yours changes before it. Then it copies the skills, puts the letter in each harness's instructions file, and installs the tools you checked. It never touches a skill it doesn't own, and it backs up everything it overwrites under `~/.squirrel/backup/`.

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/janiorvalle/squirrel/main/install.ps1 | iex
```

That puts `squirrel.exe` in `%LOCALAPPDATA%\Programs\squirrel` on your user PATH and runs `squirrel setup`, with the tool checks and installs in Windows PowerShell.

Run `squirrel setup` again any time. Your saved answers come preselected, so a rerun with nothing changed is one Enter, and a harness installed since last time shows up checked. `squirrel upgrade` fetches the newest release and reruns setup.

Two questions are asked once. A skills repo of your own, `owner/name` on GitHub with a `skills/` folder: setup clones it with gh under `~/.squirrel/repos`, pulls it every run, and installs its skills beside squirrel's in every harness you picked. And where your repos live, guessed from the folders under home that hold git checkouts: `~/code`, `~/github`, `~/src`, `~/projects`, `~/dev`. Setup lists every repo there with its `Tracker:` line or `not declared`. Then the trackers the `tracker` skill knows, over the repos that haven't named one. Linear asks by team: the team key, then a checkbox list of the repos in it, then the key again for the next team, Enter with nothing when there's no more. GitHub Issues and markdown tasks are one checkbox list each. Type `/` on any list to filter by name. A repo whose origin is a GitHub repo with issues on comes checked on the GitHub Issues list. A repo left unchecked on every list is skipped and not offered again; `--ask-trackers-again` offers it again. The plan names each repo that can take the one-line PR through gh and why the rest can't, and one question opens them all. The lines land in each repo's `AGENTS.md` after the confirm.

Without a terminal, setup prints the plan and the exact flags to apply it, and changes nothing in the harnesses (a skills repo you named is still refreshed):

```sh
squirrel setup --harness claude,codex --yes   # or --harness all
```

- `--install-tools` installs the missing tools without asking. `--update-tools` updates the outdated ones.
- `--keep-instructions` appends the letter to an instructions file that has other content instead of replacing it.
- `--skill-repo owner/name` adds a skills repo. `--forget-skill-repo` drops one.
- `--override name=owner/name` picks between two skills with the same name, which otherwise stop setup and ask.
- `--repos-dir <folder>` answers the repos question. `--ask-trackers-again` offers the repos you skipped on the tracker lists again, and asks gh again about every origin instead of using what it said on an earlier run.

## Use it

Restart your harness so the skills load. Then start any multi-step task with `/squirrel`, or say "work the squirrel way". The letter is in your instructions file, so the mode is on in every session.

## Tools

The flow leans on two tools. `squirrel setup` offers each one that's missing or behind its latest release, and an update runs through whoever installed the binary: `brew upgrade` for a Homebrew one, the `tools.md` line for one in `~/.local/bin`, the pinned npm line for an npm one. A binary anywhere else is shown with its path and left to you.

- **roast**, the independent review gate. A different model reviews the diff until it says well done. It needs TruffleHog for its secret scan, and setup installs that too.
- **agent-browser**, a real browser from the command line, for using what you built like a person would.

The work tracker isn't a tool setup installs. Each repo names its own on a `Tracker:` line in its instructions file, and the `tracker` skill has the contract, the ticket shape, and the commands for markdown tasks, GitHub Issues, and Linear.

Each tool ships its own skill and installs it. `tools.md` has the check, version, and install lines setup runs, with `~/.local/bin` on PATH so a tool just installed there is found in the same run. git and gh are prerequisites: setup checks for them and says where to get them, never installs them. The right command depends on the OS, and `gh auth login` is yours to run.

## What's in it

- `AGENTS.md` is the letter: who's who, what we're doing, and the rules that matter first. Setup installs it into your harness's user-level instructions file so the mode is always on.
- `skills/squirrel/` is the front door: the flow, the rules, and an index of every skill, generated from their description lines.
- `skills/*/` with `kind: principle` in the frontmatter are the principles. One rule each.
- The rest are workflows: `how`, `why`, `architect`, `arena`, `swarm`, `land-pr`, `worktree`, and so on.
- `tools.md` names the tools the flow expects and how to get them. A `(windows)` line runs there in PowerShell, the plain line is POSIX shell for macOS and Linux. The agent-browser line pins the CLI version because its skill text ships inside the CLI, and `scripts/tool-bump.py` moves that pin through a weekly PR.
- `vendor.json` pins the third-party skills in `skills/`, the ones whose tool squirrel doesn't control, so their text changes through a reviewed PR. `scripts/vendor-bump.py` copies each in at its pin, and a weekly workflow opens a bump PR when upstream moves. Our own tools, roast and tokenomnom, ship their skill with the binary instead.
- `cmd/squirrel` and `internal/` are the binary, with the skills, the letter, `tools.md`, and `vendor.json` embedded at build time, so setup runs from anywhere.
- This repo tracks its own work in GitHub Issues, named by the `Tracker:` line at the top of the letter. Setup leaves that line out of what it installs.
- `decisions.md` records the choices made while building this, so nobody relitigates them.

## Development

```sh
make install-hooks   # gitleaks plus the verify script on every commit
make verify          # what CI runs: gofmt, build, vet, test, then the skill checks
make index           # rebuild the letter's workflows table after changing a description
make setup           # go run ./cmd/squirrel setup, with this checkout's skills embedded
```

See `CONTRIBUTING.md` for the shape of a skill and the CLA.

## Credit

The idea of a personal agent stack with a mode as the front door, small principle files, and a fan-out pattern for understanding code comes from [pstack](https://github.com/cursor/plugins/tree/main/pstack). Everything here is written from scratch.

## License

MIT.
