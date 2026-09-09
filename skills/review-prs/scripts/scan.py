#!/usr/bin/env python3
"""List open PRs across every GitHub repo checked out under a directory.
Output: repo|#number|title|url|author, one line per PR, sorted by repo.

  scan.py [--dir DIR] [--org ORG] [--exclude PATTERN ...]

Defaults come from ~/.config/squirrel/review-prs.json when present, else
dir=~/code, no org filter, no excludes. Flags override the file.
"""
import argparse, fnmatch, json, os, re, subprocess, sys

CONFIG = os.path.expanduser("~/.config/squirrel/review-prs.json")


def settings():
    config = {}
    if os.path.isfile(CONFIG):
        with open(CONFIG, encoding="utf-8") as f:
            config = json.load(f)
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter, allow_abbrev=False)
    p.add_argument("--dir", default=config.get("dir") or "~/code")
    p.add_argument("--org", default=config.get("org", ""))
    p.add_argument("--exclude", action="append", default=list(config.get("exclude", [])), metavar="PATTERN")
    return p.parse_args()


def checkouts(root):
    """(owner, name) for every folder under root that holds a .git and an origin on github.com."""
    for entry in sorted(os.scandir(root), key=lambda e: e.name):
        if entry.name.startswith(".") or not os.path.isdir(os.path.join(entry.path, ".git")):
            continue
        remote = subprocess.run(["git", "-C", entry.path, "remote", "get-url", "origin"], capture_output=True, encoding="utf-8")
        if remote.returncode or "github.com" not in remote.stdout:
            continue
        org_repo = re.sub(r"\.git$", "", re.sub(r".*github\.com[:/]", "", remote.stdout.strip()))
        owner, _, name = org_repo.partition("/")
        yield owner, name


def open_prs(owner, name):
    listing = subprocess.run(
        ["gh", "pr", "list", "--repo", f"{owner}/{name}", "--state", "open", "--json", "number,title,url,author"],
        capture_output=True, encoding="utf-8",
    )
    if listing.returncode:
        return []
    return [f"{name}|#{pr['number']}|{pr['title']}|{pr['url']}|{pr['author']['login']}" for pr in json.loads(listing.stdout)]


def main():
    a = settings()
    if subprocess.run(["gh", "auth", "status"], capture_output=True).returncode:
        sys.exit("gh is not authenticated. Run: gh auth login")
    lines = [
        line
        for owner, name in checkouts(os.path.expanduser(a.dir))
        if (not a.org or owner == a.org) and not any(fnmatch.fnmatchcase(name, pattern) for pattern in a.exclude)
        for line in open_prs(owner, name)
    ]
    # Windows pipes default to the ANSI code page, which can't carry every PR title.
    sys.stdout.reconfigure(encoding="utf-8")
    # Repo case-insensitively, then the PR number as text: the order the shell's sort gave.
    for line in sorted(lines, key=lambda line: (line.split("|")[0].lower(), line.split("|")[1])):
        print(line)


if __name__ == "__main__":
    main()
