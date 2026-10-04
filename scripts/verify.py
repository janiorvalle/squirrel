#!/usr/bin/env python3
"""The fast gate for this repo. Runs in CI as the Verify check and in the pre-commit hook.

Checks every skill's frontmatter, that the mode's index matches the description
lines, and that no file carries an em dash or a harness name where it shouldn't.
A skill's .html files, like the mockup's feedback layer, get the same text checks.
Vendored skills (the vendor.json list) are upstream text copied verbatim, so they
only have to exist with a SKILL.md.
"""
import os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SKILLS = os.path.join(ROOT, "skills")
sys.path.insert(0, os.path.join(ROOT, "scripts"))
import importlib
build_index = importlib.import_module("build-index")

HARNESS = re.compile(r"\b(Claude Code|Claude|Cursor|Codex|Agent tool|subagent_type|generalPurpose)\b")
# Work identifiers that must never appear in this repo. The generic procedure lives
# here; anything customer- or employer-specific belongs in a private skills folder.
DENY = re.compile(r"(?i)\b(streamlyne|ekualiti|fundfit|kuali|janior-devbox|100\.57\.65\.112|Documents/github)\b")
HARNESS_ALLOWED = {"tools.md", "README.md", "CONTRIBUTING.md", "decisions.md", "skills/squirrel-setup/SKILL.md"}
SELF = "scripts/verify.py"
VENDORED = build_index.vendored()

problems = []

def check_frontmatter(name, path):
    text = open(path).read()
    if not text.startswith("---\n"):
        problems.append(f"{path}: no frontmatter"); return
    head = text.split("---\n", 2)[1]
    m = re.search(r"^name:\s*(\S+)\s*$", head, re.M)
    if not m:
        problems.append(f"{path}: no name line")
    elif m.group(1) != name:
        problems.append(f"{path}: name is {m.group(1)}, folder is {name}")
    if not re.search(r"^description:\s*\S", head, re.M):
        problems.append(f"{path}: no description line")
    k = re.search(r"^kind:\s*(\S+)\s*$", head, re.M)
    if k and k.group(1) != "principle":
        problems.append(f"{path}: kind must be principle or absent, got {k.group(1)}")

def is_vendored(rel):
    parts = rel.split("/")
    return parts[0] == "skills" and len(parts) > 1 and parts[1] in VENDORED

def check_text(rel, path):
    if rel == SELF or is_vendored(rel):
        return
    text = open(path).read()
    for i, line in enumerate(text.splitlines(), 1):
        if "—" in line:
            problems.append(f"{rel}:{i}: em dash")
        if rel not in HARNESS_ALLOWED and HARNESS.search(line):
            problems.append(f"{rel}:{i}: harness name: {line.strip()[:80]}")
        if DENY.search(line):
            problems.append(f"{rel}:{i}: work identifier: {line.strip()[:80]}")

def main():
    for name in sorted(VENDORED):
        if not os.path.isfile(os.path.join(SKILLS, name, "SKILL.md")):
            problems.append(f"skills/{name}/SKILL.md: missing, but vendor.json lists {name}. Run scripts/vendor-bump.py restore {name}")
    for name in sorted(os.listdir(SKILLS)):
        skill = os.path.join(SKILLS, name, "SKILL.md")
        if os.path.isdir(os.path.join(SKILLS, name)) and not os.path.isfile(skill):
            problems.append(f"skills/{name}: no SKILL.md")
        elif os.path.isfile(skill) and name not in VENDORED:
            check_frontmatter(name, skill)
    for dirpath, _, files in os.walk(ROOT):
        if "/.git" in dirpath or "__pycache__" in dirpath:
            continue
        for f in files:
            path = os.path.join(dirpath, f)
            rel = os.path.relpath(path, ROOT)
            if f.endswith((".md", ".py", ".sh", ".json")) or (f.endswith(".html") and rel.startswith("skills/")):
                check_text(rel, path)
    mode = open(build_index.MODE).read()
    start, end = "<!-- index:start -->", "<!-- index:end -->"
    current = mode.split(start, 1)[1].split(end, 1)[0].strip()
    if current != build_index.build().strip():
        problems.append("AGENTS.md: workflows table is stale, run make index")
    if problems:
        print("\n".join(problems)); sys.exit(1)
    print("verify: ok")

if __name__ == "__main__":
    main()
