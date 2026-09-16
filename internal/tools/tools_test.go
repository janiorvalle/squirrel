package tools

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/janiorvalle/squirrel"
)

const sample = "# Tools\n\nIntro with a `- Check:` mention that is not a section.\n\n" +
	"## git and gh\n\n- Check: `command -v git && gh auth status`\n- Install: `brew install git gh`, then `gh auth login`\n\n" +
	"## The work tracker\n\nProse.\n\n**Quest** (current)\n- Repo: https://github.com/x/quest\n- Check: `command -v quest`\n- Version: `quest --version`\n- Install: `curl -fsSL https://x/install.sh | sh`\n- Skill install: `quest skill install`\n- Skill folder: `quest`\n\n" +
	"## no check here\n\n- Install: `something`\n\n" +
	"## bare\n\n- Check: `command -v bare`\n"

func TestParseKeepsSectionsWithACheckLine(t *testing.T) {
	parsed := Parse(sample)
	if len(parsed) != 3 {
		t.Fatalf("parsed %d tools, want 3: %+v", len(parsed), parsed)
	}
	git := parsed[0]
	if git.Title != "git and gh" || git.Check != "command -v git && gh auth status" {
		t.Fatalf("git = %+v", git)
	}
	if git.Install != "brew install git gh, then gh auth login" || git.Command != "brew install git gh && gh auth login" {
		t.Fatalf("git install = %q, command = %q", git.Install, git.Command)
	}
	if git.SkillInstall != "" || git.SkillFolder != "" || git.Version != "" || git.Repo != "" {
		t.Fatalf("git skill = %q %q, version = %q, repo = %q", git.SkillInstall, git.SkillFolder, git.Version, git.Repo)
	}
	quest := parsed[1]
	if quest.Title != "The work tracker" || quest.Command != "curl -fsSL https://x/install.sh | sh" || quest.SkillInstall != "quest skill install" || quest.SkillFolder != "quest" {
		t.Fatalf("quest = %+v", quest)
	}
	if quest.Version != "quest --version" || quest.Repo != "https://github.com/x/quest" {
		t.Fatalf("quest version = %q, repo = %q", quest.Version, quest.Repo)
	}
	bare := parsed[2]
	if bare.Install != "see https://github.com/janiorvalle/squirrel/blob/main/tools.md#bare" || bare.Command != "" {
		t.Fatalf("bare = %+v", bare)
	}
}

func TestPrerequisiteLinksToItsOwnHeading(t *testing.T) {
	prerequisite := "## git and gh\n\nProse.\n\n- Check: `command -v git`\n- macOS: `brew install git gh`\n- Then `gh auth login`\n"
	parsed := Parse(prerequisite)
	if len(parsed) != 1 {
		t.Fatalf("parsed %d tools, want 1: %+v", len(parsed), parsed)
	}
	if parsed[0].Command != "" {
		t.Fatalf("a prerequisite has a command to run: %q", parsed[0].Command)
	}
	if parsed[0].Install != "see https://github.com/janiorvalle/squirrel/blob/main/tools.md#git-and-gh" {
		t.Fatalf("install = %q", parsed[0].Install)
	}
}

func TestParseReadsThePinFromAnNpmInstallLine(t *testing.T) {
	for line, want := range map[string]string{
		"`npm install -g agent-browser@0.36.0 && agent-browser install`": "v0.36.0",
		"`npm install -g agent-browser && agent-browser install`":        "",
		"`npm install -g agent-browser@latest`":                          "",
		"`curl -fsSL https://x/install.sh | sh`":                         "",
	} {
		parsed := Parse("## t\n\n- Check: `command -v t`\n- Install: " + line + "\n")
		if got := parsed[0].Pin; got != want {
			t.Errorf("Pin for %s = %q, want %q", line, got, want)
		}
	}
}

func TestParseReadsTheBinaryFromAOneCommandCheckLine(t *testing.T) {
	for check, want := range map[string]string{
		"command -v roast":  "roast",
		"Get-Command roast": "roast",
		"command -v git && command -v gh && gh auth status":     "",
		"Get-Command git, gh -ErrorAction Stop; gh auth status": "",
	} {
		if got := binaryFrom(check); got != want {
			t.Fatalf("binaryFrom(%q) = %q, want %q", check, got, want)
		}
	}
	parsed := parseFor("## roast\n\n- Check: `command -v roast`\n- Check (windows): `Get-Command roast`\n", "windows")
	if len(parsed) != 1 || parsed[0].Binary != "roast" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestNpmInstalledReadsTheInstallLine(t *testing.T) {
	if !(Tool{Command: "npm install -g agent-browser@0.36.0 && agent-browser install"}).NpmInstalled() {
		t.Fatal("an npm install line is not npm installed")
	}
	if (Tool{Command: "curl -fsSL https://x/install.sh | sh"}).NpmInstalled() || (Tool{}).NpmInstalled() {
		t.Fatal("a curl line or no line is npm installed")
	}
}

func TestParseEmptyText(t *testing.T) {
	if got := Parse(""); len(got) != 0 {
		t.Fatalf("parsed %+v", got)
	}
}

func TestParseVersionFindsTheVersionInWhatToolsPrint(t *testing.T) {
	for output, want := range map[string]string{
		"quest 0.24.0\n":                 "v0.24.0",
		"0.2.5":                          "v0.2.5",
		"roast 1.7.0":                    "v1.7.0",
		"tokenomnom version 0.6.6":       "v0.6.6",
		"agent-browser 0.27.0":           "v0.27.0",
		"v1.2.3-rc.1":                    "v1.2.3-rc.1",
		"roast 1.2.3 (commit abc1234)\n": "v1.2.3",
		"usage: roast [command]":         "",
		"":                               "",
		"go version go1.24":              "",
		"version 01.2.3 is not semver":   "",
	} {
		if got := ParseVersion(output); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", output, got, want)
		}
	}
}

func TestOutdatedNeedsBothVersions(t *testing.T) {
	for name, tc := range map[string]struct {
		installed, latest string
		want              bool
	}{
		"behind":            {"v1.0.0", "v1.1.0", true},
		"behind by a patch": {"v1.7.0", "v1.7.1", true},
		"current":           {"v1.1.0", "v1.1.0", false},
		"ahead":             {"v1.2.0", "v1.1.0", false},
		"prerelease behind": {"v1.1.0-rc.1", "v1.1.0", true},
		"latest unknown":    {"v1.0.0", "", false},
		"installed unknown": {"", "v1.1.0", false},
		"both unknown":      {"", "", false},
	} {
		if got := Outdated(tc.installed, tc.latest); got != tc.want {
			t.Errorf("%s: Outdated(%q, %q) = %v, want %v", name, tc.installed, tc.latest, got, tc.want)
		}
	}
}

func TestDisplayDropsTheV(t *testing.T) {
	if got := Display("v1.2.3"); got != "1.2.3" {
		t.Fatalf("Display = %q", got)
	}
}

const perOS = "## quest\n\n- Check: `command -v quest`\n- Check (windows): `Get-Command quest`\n- Version: `quest --version`\n" +
	"- Install: `curl -fsSL https://x/install.sh | sh`\n- Install (windows): `irm https://x/install.ps1 | iex`\n- Skill install: `quest skill install`\n\n" +
	"## roast\n\n- Check: `command -v roast`\n- Check (windows): `Get-Command roast`\n- Install: `curl -fsSL https://x/roast.sh | sh`\n- Install (windows): download the zip from https://x/releases and put roast.exe on PATH\n"

func TestOSLineWinsOnItsOSAndThePlainLineElsewhere(t *testing.T) {
	for goos, want := range map[string]Tool{
		"darwin":  {Check: "command -v quest", Install: "curl -fsSL https://x/install.sh | sh", Command: "curl -fsSL https://x/install.sh | sh"},
		"linux":   {Check: "command -v quest", Install: "curl -fsSL https://x/install.sh | sh", Command: "curl -fsSL https://x/install.sh | sh"},
		"windows": {Check: "Get-Command quest", Install: "irm https://x/install.ps1 | iex", Command: "irm https://x/install.ps1 | iex"},
	} {
		quest := parseFor(perOS, goos)[0]
		if quest.Check != want.Check || quest.Install != want.Install || quest.Command != want.Command {
			t.Errorf("%s: check = %q, install = %q, command = %q", goos, quest.Check, quest.Install, quest.Command)
		}
		if quest.Version != "quest --version" || quest.SkillInstall != "quest skill install" {
			t.Errorf("%s: lines without an OS variant changed: %+v", goos, quest)
		}
	}
}

func TestWindowsInstallWrittenForAPersonIsShownAndNeverRun(t *testing.T) {
	roast := parseFor(perOS, "windows")[1]
	if roast.Command != "" || roast.Install != "download the zip from https://x/releases and put roast.exe on PATH" {
		t.Fatalf("roast on windows = %+v", roast)
	}
	if unix := parseFor(perOS, "linux")[1]; unix.Command != "curl -fsSL https://x/roast.sh | sh" {
		t.Fatalf("roast on linux = %+v", unix)
	}
}

// Every section of the real tools.md needs a Windows check, or a tool that
// is present shows as missing there: PowerShell has no command -v.
func TestEveryRealToolHasAWindowsCheckLine(t *testing.T) {
	markdown, err := fs.ReadFile(squirrel.Files, "tools.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range parseFor(string(markdown), "windows") {
		if strings.Contains(tool.Check, "command -v") {
			t.Errorf("%s: windows check is the POSIX line %q", tool.Title, tool.Check)
		}
		if strings.Contains(tool.Command, " && ") || strings.Contains(tool.Command, "| sh") {
			t.Errorf("%s: windows install is the POSIX line %q", tool.Title, tool.Command)
		}
	}
}

// A curl line that pipes into sh reports installed when the download fails:
// sh runs nothing and exits zero. Every real line downloads to a file and
// runs the file, so curl's failure is the line's.
func TestEveryRealCurlInstallLineDownloadsBeforeItRuns(t *testing.T) {
	markdown, err := fs.ReadFile(squirrel.Files, "tools.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range parseFor(string(markdown), "linux") {
		if !strings.Contains(tool.Command, "curl ") {
			continue
		}
		if strings.Contains(tool.Command, "| sh") || !strings.Contains(tool.Command, `curl -fsSL -o "$script"`) {
			t.Errorf("%s: install line pipes into sh instead of downloading first: %q", tool.Title, tool.Command)
		}
	}
}
