#!/usr/bin/env python3
"""Runs the skill scripts CI exercises on every OS it builds for.

The worktree lock goes through claim, heartbeat, list, and release against a
throwaway registry, and the review-prs scan walks a folder holding one throwaway
checkout whose origin is this checkout's origin, so the scan lists this repo's
open PRs and they are checked against gh's own list, then once more with a bad
token to see the scan stop the way the skill says.
Each command is printed before its output, so the CI log reads as a transcript.
"""
import json, os, subprocess, sys, tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOCK = os.path.join(ROOT, "skills", "worktree", "scripts", "worktree_lock.py")
SCAN = os.path.join(ROOT, "skills", "review-prs", "scripts", "scan.py")


def run(*args, env=None):
    print("$", " ".join(args), flush=True)
    out = subprocess.run(args, check=True, env=env, stdout=subprocess.PIPE, encoding="utf-8").stdout
    print(out, end="", flush=True)
    return out


def main():
    work = tempfile.mkdtemp(prefix="squirrel-skills-smoke-", dir=os.environ.get("RUNNER_TEMP"))
    env = {**os.environ, "SQUIRREL_WORKTREE_REGISTRY": os.path.join(work, "worktree-locks.json")}
    path = os.path.join(work, "demo")
    run(sys.executable, LOCK, "claim", "--repo", "squirrel", "--path", path, "--branch", "demo", "--base-ref", "origin/main", "--owner", "ci", "--purpose", "smoke: the lock script runs here", env=env)
    run(sys.executable, LOCK, "heartbeat", "--path", path, "--notes", "still here", env=env)
    active = json.loads(run(sys.executable, LOCK, "list", "--status", "active", env=env))
    assert [(lock["path"], lock["notes"]) for lock in active] == [(path, "still here")], active
    run(sys.executable, LOCK, "release", "--path", path, "--status", "released", env=env)
    assert json.loads(run(sys.executable, LOCK, "list", "--status", "active", env=env)) == []

    origin = run("git", "-C", ROOT, "remote", "get-url", "origin").strip()
    scan_dir = os.path.join(work, "scan")
    checkout = os.path.join(scan_dir, "squirrel")
    run("git", "init", "-q", checkout)
    run("git", "-C", checkout, "remote", "add", "origin", origin)
    listed = {f"#{pr['number']}" for pr in json.loads(run("gh", "pr", "list", "--repo", origin, "--state", "open", "--json", "number"))}
    lines = run(sys.executable, SCAN, "--dir", scan_dir).splitlines()
    assert {line.split("|")[1] for line in lines} == listed and len(lines) == len(listed), (lines, listed)
    for line in lines:
        assert line.startswith("squirrel|#") and line.count("|") >= 4, line

    print("$ GH_TOKEN=bad", sys.executable, SCAN, "--dir", scan_dir, flush=True)
    unauthenticated = subprocess.run(
        [sys.executable, SCAN, "--dir", scan_dir],
        env={**os.environ, "GH_TOKEN": "bad", "GH_CONFIG_DIR": os.path.join(work, "no-gh-config")},
        capture_output=True, encoding="utf-8",
    )
    print(unauthenticated.stderr, end="", flush=True)
    assert unauthenticated.returncode == 1 and unauthenticated.stderr.strip() == "gh is not authenticated. Run: gh auth login", unauthenticated


if __name__ == "__main__":
    main()
