# Tools

squirrel expects a few tools on the machine. Each ships its own skill, which the tool maintains, so squirrel doesn't copy those. This file names each tool, what it's for, and how to get it.

If a tool is missing, run the check, show the install command, and install only when the human says yes or the session was already given permission to set things up. The install command is this file's `Install` line, the one `squirrel setup` runs, never the one inside the tool's own skill. Then read the skill the tool installed and follow it.

`squirrel setup` runs every check below, installs each present tool's skill, and reports each tool as missing, outdated, or current. It parses the `Check`, `Version`, `Repo`, `Install`, `Skill install`, and `Skill folder` lines:

- `Version` prints the installed version. The latest comes from the GitHub releases of the `Repo` line, or from npm when the install line is `npm install -g`.
- An update runs through whoever installed the binary. `Check` is `command -v <binary>`, so setup asks the shell where the binary is and follows its links. Under node's global `node_modules` for an npm install line: that line. A link into Homebrew's Cellar: `brew upgrade <formula>`, the formula read off the path. A file in `~/.local/bin`: the `Install` line. Anything else, a file in Homebrew's bin that isn't brew's link included: shown with its path and left alone, since the install line would drop a second copy that loses on PATH.
- A pinned line, `npm install -g name@1.2.3`, makes the pin the latest: behind it is outdated, past it is ahead, the update installs the pin either way, and the weekly vendor-bump workflow opens a PR when npm publishes a newer one.
- A `Check` line with no `Install` line is a prerequisite. Setup checks for it and points here when it's missing, never installs or updates it. git and gh are the prerequisites.
- Setup runs the lines in the shell the OS ships with: `sh` on macOS and Linux, Windows PowerShell on Windows. A line is POSIX shell unless it carries an OS suffix, `Check (windows)`, which wins on that OS and runs in Windows PowerShell. The suffix is Go's OS name: `windows`, `darwin`, or `linux`. A command that's the same in both shells, `roast --version`, carries no suffix.
- Windows lines use `;` between steps, since Windows PowerShell has no `&&`, and single quotes, since setup passes the line inside double quotes.
- An `Install` line with no backticks is a step for a person. Setup shows it and never runs it.
- An `Install` line that fetches a script downloads it to a file, then runs the file, so a failed download fails the line. `curl | sh` exits zero when curl gets nothing, and setup would report the tool installed.

## git and gh

Version control and the PR host CLI. Every flow assumes both. Setup checks for them and never installs them, since the command depends on the OS and its package manager, Linux needs sudo, and `gh auth login` is yours to run. Get them by hand, then rerun `squirrel setup`.

- Check: `command -v git && command -v gh && gh auth status`
- Check (windows): `Get-Command git, gh -ErrorAction Stop; gh auth status`
- macOS: `brew install git gh`
- Debian and Ubuntu: `sudo apt install git gh`
- Fedora: `sudo dnf install git gh`
- Windows: `winget install --id Git.Git -e; winget install --id GitHub.cli -e`
- Anything else: https://github.com/cli/cli#installation covers gh, and git comes from the same package manager
- Then `gh auth login`

## roast

The independent code review gate. A different model than the one that wrote the diff reviews it, until it says well done.

- Repo: https://github.com/janiorvalle/roast
- Check: `command -v roast`
- Check (windows): `Get-Command roast`
- Version: `roast --version`
- Install: `script=$(mktemp) && curl -fsSL -o "$script" https://raw.githubusercontent.com/janiorvalle/roast/main/install.sh && sh "$script"`
- Install (windows): `irm https://raw.githubusercontent.com/janiorvalle/roast/main/install.ps1 | iex`
- Skill install: `roast install-skill --force`
- Skill folder: `roast`
- The skill covers the scope fence and the loop until well done. roast installs it on its own the first time it runs in a repo.
- roast refuses to run without TruffleHog on PATH, its `ROAST-SECRET-BINARY` error. Its installer doesn't bring it, so setup installs it from the section below.

## TruffleHog

Secret scanner, run by roast over the diff before review. On macOS and Linux its official installer drops one binary into `~/.local/bin`, which the squirrel installer put on your PATH, no sudo. Upstream ships no Windows installer, so squirrel embeds one, `scripts/install-trufflehog.ps1`. Setup writes it to `~/.squirrel/scripts` before any tool line runs, and the Windows install line runs it from there: it downloads the release tar.gz, verifies the SHA-256 against the release's checksums file, and puts `trufflehog.exe` in `%LOCALAPPDATA%\Programs\trufflehog` on your user PATH. TruffleHog ships no skill.

- Repo: https://github.com/trufflesecurity/trufflehog
- Check: `command -v trufflehog`
- Check (windows): `Get-Command trufflehog`
- Version: `trufflehog --version`
- Install: `script=$(mktemp) && curl -fsSL -o "$script" https://raw.githubusercontent.com/trufflesecurity/trufflehog/main/scripts/install.sh && sh "$script" -b ~/.local/bin`
- Install (windows): `Get-Content -Raw (Join-Path $env:USERPROFILE '.squirrel\scripts\install-trufflehog.ps1') | iex`

## tokenomnom

Token usage and spend across your coding agents, plus transcript search. Not part of the flow, but the tools group ships it.

- Repo: https://github.com/janiorvalle/tokenomnom
- Check: `command -v tokenomnom`
- Check (windows): `Get-Command tokenomnom`
- Version: `tokenomnom --version`
- Install: `script=$(mktemp) && curl -fsSL -o "$script" https://raw.githubusercontent.com/janiorvalle/tokenomnom/main/install.sh && sh "$script"`
- Install (windows): `irm https://raw.githubusercontent.com/janiorvalle/tokenomnom/main/install.ps1 | iex`
- Skill install: `tokenomnom install-skill`
- Skill folder: `tokenomnom`

## agent-browser

Drives a real browser from the command line, so an agent uses a web UI the way a person would and captures the screenshots the flow requires. Required in every harness.

- Repo: https://github.com/vercel-labs/agent-browser
- Check: `command -v agent-browser`
- Check (windows): `Get-Command agent-browser`
- Version: `agent-browser --version`
- Install: `npm install -g agent-browser@0.37.1 && agent-browser install`
- Install (windows): `npm install -g agent-browser@0.37.1; agent-browser install`
- Its skill ships in this repo under `skills/agent-browser`, copied from upstream at the commit pinned in `vendor.json` because squirrel doesn't control the tool, and `squirrel setup` installs it like every other skill. It's a stub that runs `agent-browser skills get core`, so the instructions agents follow ship inside the CLI, which is why the install line pins the CLI version and `scripts/tool-bump.py` moves it through a PR.
- The stub's `npm i -g agent-browser` is upstream text and installs whatever is newest, not the pin. Use the `Install` line above or `squirrel setup --install-tools`. Setup reports any other version as outdated or ahead.

Codex ships its own `playwright-interactive` skill for persistent browser and Electron sessions. It's Codex's, not this stack's, and stays wherever Codex put it.
