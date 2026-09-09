#!/usr/bin/env python3
"""Tear down one worktree: its compose stack, its lock, the tree, the local
branch. Then stop. Only the human runs this, and only through /nuke.

    nuke.py [worktree-path]          defaults to the worktree you're in

Refuses when the branch has commits the remote doesn't, since that's the one
thing nuke can't undo. Everything else goes."""
import json, os, subprocess, sys

def sh(*args, cwd=None, check=True):
    return subprocess.run(args, cwd=cwd, capture_output=True, text=True, check=check).stdout.strip()

def die(message):
    print(f"NUKE-STOP: {message}", file=sys.stderr); sys.exit(2)

def worktree_root(start):
    try:
        return os.path.realpath(sh("git", "rev-parse", "--show-toplevel", cwd=start))
    except subprocess.CalledProcessError:
        die(f"{start} is not inside a git worktree")

def main_checkout(tree):
    common = sh("git", "rev-parse", "--git-common-dir", cwd=tree)
    return os.path.dirname(os.path.realpath(os.path.join(tree, common)))

def unpushed(tree):
    """Commits the remote doesn't have. With no upstream, the lock's base ref
    stands in. With neither, nuke can't tell, so it refuses rather than guess."""
    try:
        against = sh("git", "rev-parse", "--abbrev-ref", "@{upstream}", cwd=tree)
    except subprocess.CalledProcessError:
        against = lock_field(tree, "base_ref")
        if not against:
            die("the branch has no upstream and no lock records its base, so nuke can't tell what's pushed; push the branch or set its upstream first")
    try:
        return sh("git", "log", "--oneline", f"{against}..HEAD", cwd=tree).splitlines(), against
    except subprocess.CalledProcessError:
        die(f"can't compare against {against}; fetch it or push the branch first")

def registry():
    path = os.environ.get("SQUIRREL_WORKTREE_REGISTRY") or os.path.expanduser("~/.config/squirrel/worktree-locks.json")
    try:
        return json.load(open(path)).get("locks", [])
    except FileNotFoundError:
        return []

def lock_for(tree):
    for lock in registry():
        if os.path.realpath(lock.get("path", "")) == os.path.realpath(tree):
            return lock
    return None

def lock_field(tree, field):
    lock = lock_for(tree)
    return lock.get(field) if lock else None

def compose_projects_under(tree):
    """Compose projects whose config files live inside this worktree, plus the
    one its .env names. Nothing else, so the shared checkout's stack is safe."""
    tree = os.path.realpath(tree) + os.sep
    names = set()
    try:
        rows = json.loads(sh("docker", "compose", "ls", "--all", "--format", "json") or "[]")
    except (subprocess.CalledProcessError, FileNotFoundError, json.JSONDecodeError):
        return names, "docker compose not available"
    for row in rows:
        files = row.get("ConfigFiles", "")
        if any(os.path.realpath(f).startswith(tree) for f in files.split(",") if f):
            names.add(row["Name"])
    env = os.path.join(tree, ".env")
    if os.path.exists(env):
        for line in open(env):
            if line.startswith("COMPOSE_PROJECT_NAME="):
                names.add(line.split("=", 1)[1].strip().strip('"\''))
    return names, None

def main():
    start = os.path.abspath(sys.argv[1]) if len(sys.argv) > 1 else os.getcwd()
    tree = worktree_root(start)
    main_root = main_checkout(tree)
    if os.path.abspath(tree) == main_root:
        die(f"{tree} is the main checkout, not a worktree; nuke only removes worktrees")
    branch = sh("git", "rev-parse", "--abbrev-ref", "HEAD", cwd=tree)
    ahead, against = unpushed(tree)
    if ahead:
        die(f"{branch} has {len(ahead)} commit(s) the remote doesn't ({against}); push them or delete them yourself first:\n  " + "\n  ".join(ahead))
    dirty = len(sh("git", "status", "--porcelain", cwd=tree).splitlines())

    projects, note = compose_projects_under(tree)
    stack_line = note or "no compose project under this worktree"
    if projects:
        for name in sorted(projects):
            sh("docker", "compose", "-p", name, "down", "--volumes", "--remove-orphans", check=False)
        stack_line = "down with volumes: " + ", ".join(sorted(projects))

    lock_line = "no lock in the registry"
    lock = lock_for(tree)
    if lock:
        script = os.path.join(os.path.dirname(__file__), "..", "..", "worktree", "scripts", "worktree_lock.py")
        sh(sys.executable, script, "release", "--path", lock["path"], "--status", "released", check=False)
        lock_line = "released"

    os.chdir(main_root)
    sh("git", "worktree", "remove", "--force", tree)
    branch_line = f"{branch} kept, it's the main branch"
    if branch not in ("main", "master"):
        sh("git", "branch", "-D", branch, check=False)
        branch_line = f"{branch} deleted locally; the remote branch and any PR are untouched"

    print(f"stack     {stack_line}")
    print(f"worktree  {tree} removed" + (f", {dirty} uncommitted file(s) went with it" if dirty else ""))
    print(f"branch    {branch_line}")
    print(f"lock      {lock_line}")

if __name__ == "__main__":
    main()
