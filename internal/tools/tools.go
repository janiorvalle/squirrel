// Package tools reads tools.md, the list of tools the flow expects on the
// machine, into the lines setup runs and shows.
package tools

import (
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

// Doc is where tools.md is read by a human; a tool with no install line
// points at its own section there.
const Doc = "https://github.com/janiorvalle/squirrel/blob/main/tools.md"

// Tool is one section of tools.md that carries a Check line. Version is the
// command that prints the installed version and Repo is the tool's GitHub
// page; both are empty for tools setup never updates, such as git. Command
// is empty for a prerequisite, a section with no Install line: Install then
// names its section of Doc, the only thing setup can offer for it. Pin is
// the version the Install line installs when it names one, as "v1.2.3", so
// setup compares against it instead of asking a registry; moving it is a PR.
// Binary is the executable the Check line looks for, "command -v roast" or
// "Get-Command roast", so setup can ask where it is and who put it there;
// a Check line shaped any other way gives "".
type Tool struct {
	Title        string
	Check        string
	Binary       string
	Version      string
	Repo         string
	Install      string
	Command      string
	Pin          string
	SkillInstall string
	SkillFolder  string
}

var (
	versionToken = regexp.MustCompile(`v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)
	npmPin       = regexp.MustCompile(`^npm install -g [^@\s]+@(\S+)`)
	backtickSpan = regexp.MustCompile("`([^`]+)`")
	binaryCheck  = regexp.MustCompile(`^(?:command -v|Get-Command) (\S+)$`)
)

// Parse returns the tools in tools.md order for the OS the binary runs on.
// Sections without a Check line are prose and are skipped.
func Parse(markdown string) []Tool {
	return parseFor(markdown, runtime.GOOS)
}

// parseFor reads each section's lines for one OS. A line such as
// "- Check (windows): ..." wins on that OS over the plain "- Check: ..." line,
// which is what every other OS gets.
func parseFor(markdown, operatingSystem string) []Tool {
	var parsed []Tool
	for _, section := range regexp.MustCompile(`(?m)^## `).Split(markdown, -1)[1:] {
		title, _, _ := strings.Cut(section, "\n")
		check := quoted(lineFor(section, "Check", operatingSystem))
		if check == "" {
			continue
		}
		install := "see " + Doc + "#" + anchor(title)
		command := ""
		if line := lineFor(section, "Install", operatingSystem); line != "" {
			install = strings.ReplaceAll(strings.TrimSpace(line), "`", "")
			command = commandFrom(line)
		}
		parsed = append(parsed, Tool{
			Title:        strings.TrimSpace(title),
			Check:        check,
			Binary:       binaryFrom(check),
			Version:      quoted(lineFor(section, "Version", operatingSystem)),
			Repo:         strings.TrimSpace(lineFor(section, "Repo", operatingSystem)),
			Install:      install,
			Command:      command,
			Pin:          pinFrom(command),
			SkillInstall: quoted(lineFor(section, "Skill install", operatingSystem)),
			SkillFolder:  quoted(lineFor(section, "Skill folder", operatingSystem)),
		})
	}
	return parsed
}

// lineFor is the text after "- <kind>: " in a section, for one OS: the line
// suffixed with that OS wins, otherwise the plain line, otherwise "".
func lineFor(section, kind, operatingSystem string) string {
	pattern := regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(kind) + `(?: \(([a-z0-9]+)\))?: (.+)$`)
	plain := ""
	for _, match := range pattern.FindAllStringSubmatch(section, -1) {
		switch match[1] {
		case operatingSystem:
			return match[2]
		case "":
			plain = match[2]
		}
	}
	return plain
}

// quoted is the text inside a line's first backtick span, the way Check,
// Version, and the skill lines carry one command each.
func quoted(line string) string {
	match := backtickSpan.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	return match[1]
}

// anchor is the fragment GitHub gives a heading, so "git and gh" links to
// #git-and-gh.
func anchor(title string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(title)), " ", "-")
}

// commandFrom joins the backtick spans of an Install line into one shell
// line, so "`brew install git gh`, then `gh auth login`" runs both steps. A
// line with no backticks is a step for a person and gives no command.
func commandFrom(line string) string {
	var steps []string
	for _, match := range backtickSpan.FindAllStringSubmatch(line, -1) {
		steps = append(steps, match[1])
	}
	return strings.Join(steps, " && ")
}

// binaryFrom reads the executable a one-command Check line looks for. The
// prerequisites' line checks several things at once and names no single
// binary, which is fine: setup never updates a prerequisite.
func binaryFrom(check string) string {
	match := binaryCheck.FindStringSubmatch(check)
	if match == nil {
		return ""
	}
	return match[1]
}

// NpmInstalled reports whether the tool's install line is an npm install,
// so the binary belongs to npm wherever node put it.
func (t Tool) NpmInstalled() bool {
	return npmPackage.MatchString(t.Command)
}

// pinFrom reads the version an "npm install -g name@1.2.3" line pins. Any
// other install line, or an npm tag such as @latest, pins nothing.
func pinFrom(command string) string {
	match := npmPin.FindStringSubmatch(command)
	if match == nil {
		return ""
	}
	return ParseVersion(match[1])
}

// ParseVersion picks the version out of what a tool's Version command
// printed, so "roast 1.7.0" and "tokenomnom version 0.6.6" both work, and
// returns it with the leading v that semver expects. Output with no version
// in it gives "".
func ParseVersion(output string) string {
	found := versionToken.FindString(output)
	if found == "" {
		return ""
	}
	if !strings.HasPrefix(found, "v") {
		found = "v" + found
	}
	if !semver.IsValid(found) {
		return ""
	}
	return found
}

// Outdated reports whether installed is behind latest. When either side is
// unknown the answer is no: setup only offers an update it can name.
func Outdated(installed, latest string) bool {
	return installed != "" && latest != "" && semver.Compare(installed, latest) < 0
}

// Display shows a version the way the tools print it, without the leading v.
func Display(version string) string {
	return strings.TrimPrefix(version, "v")
}
