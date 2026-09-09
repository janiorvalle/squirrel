#!/usr/bin/env python3
"""Runs the skill scripts CI exercises on every OS it builds for.

The worktree lock goes through claim, heartbeat, list, and release against a
throwaway registry, and the review-prs scan walks a folder holding one throwaway
checkout whose origin is this checkout's origin, so gh lists this repo's open PRs.
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
    for line in run(sys.executable, SCAN, "--dir", scan_dir).splitlines():
        assert line.startswith("squirrel|#") and line.count("|") >= 4, line


if __name__ == "__main__":
    main()
