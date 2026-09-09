#!/usr/bin/env python3
"""Tear down one worktree: its compose stack, its lock, the tree, the local
branch. Then stop. Only the human runs this, and only through /nuke.

    nuke.py [worktree-path]          defaults to the worktree you're in

Refuses when the branch has commits the remote doesn't, since that's the one
thing nuke can't undo. Everything else goes."""
import json, os, re, shutil, subprocess, sys

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

def inside(path, tree):
    """True for the tree itself and anything under it, by real path."""
    path, tree = os.path.realpath(path), os.path.realpath(tree)
    return path == tree or path.startswith(tree + os.sep)

def processes_for(tree, ports):
    """Processes whose working directory is inside the tree, plus whatever is
    listening on the ports the lock recorded: dev servers, watchers, shells an
    agent left behind. Never this script's own process tree."""
    mine = {os.getpid(), os.getppid()}
    found = {}
    have_lsof = shutil.which("lsof") is not None
    if have_lsof:
        for line in sh("lsof", "-nP", "-d", "cwd", "-Fpn", check=False).splitlines():
            if line.startswith("p"): pid = int(line[1:])
            elif line.startswith("n") and inside(line[1:], tree) and pid not in mine:
                found[pid] = f"cwd {line[1:]}"
    elif os.path.isdir("/proc"):
        for entry in os.listdir("/proc"):
            if not entry.isdigit() or int(entry) in mine: continue
            try: cwd = os.readlink(f"/proc/{entry}/cwd")
            except OSError: continue
            if inside(cwd, tree): found[int(entry)] = f"cwd {cwd}"
    else:
        die("neither lsof nor /proc is available, so nuke can't see what runs in this tree; install lsof first")
    for name, port in (ports or {}).items():
        if have_lsof:
            pids = sh("lsof", "-nP", "-t", f"-iTCP:{port}", "-sTCP:LISTEN", check=False).split()
        elif shutil.which("ss"):
            pids = re.findall(r"pid=(\d+)", sh("ss", "-ltnp", f"sport = :{port}", check=False))
        else:
            die(f"neither lsof nor ss is available, so nuke can't see who listens on port {port}")
        for pid in pids:
            if int(pid) not in mine: found[int(pid)] = f"listening on {name} port {port}"
    return found

def pid_alive(pid):
    try: os.kill(pid, 0); return True
    except ProcessLookupError: return False
    except PermissionError: return True

def kill(pids):
    import signal, time
    for pid in pids:
        try: os.kill(pid, signal.SIGTERM)
        except ProcessLookupError: pass
    time.sleep(1)
    for pid in pids:
        try: os.kill(pid, signal.SIGKILL)
        except ProcessLookupError: pass

def containers_mounting(tree):
    """Plain containers, compose or not, with a bind mount from inside the
    tree, and the named volumes they use, which `docker rm -v` leaves behind."""
    names, volumes = [], []
    if not shutil.which("docker"):
        return names, volumes
    listing = subprocess.run(["docker", "ps", "-aq"], capture_output=True, text=True)
    if listing.returncode != 0:
        die(f"docker is installed but not answering; nuke can't list containers ({listing.stderr.strip()[:120]})")
    ids = listing.stdout.split()
    if not ids: return names, volumes
    try:
        containers = json.loads(sh("docker", "inspect", *ids))
    except (subprocess.CalledProcessError, json.JSONDecodeError) as e:
        die(f"docker inspect failed; nuke can't tell which containers use this tree ({str(e)[:120]})")
    for c in containers:
        mounts = c.get("Mounts", [])
        if any(inside(m.get("Source", ""), tree) for m in mounts if m.get("Type") == "bind"):
            names.append(c["Name"].lstrip("/"))
            volumes += [m["Name"] for m in mounts if m.get("Type") == "volume" and m.get("Name")]
    return names, volumes

def compose_projects_under(tree):
    """Compose projects whose config files live inside this worktree. Only
    that: a project name in the tree's .env proves nothing, since a copied
    .env can name the main checkout's project."""
    names = set()
    try:
        rows = json.loads(sh("docker", "compose", "ls", "--all", "--format", "json") or "[]")
    except FileNotFoundError:
        return names
    except (subprocess.CalledProcessError, json.JSONDecodeError) as e:
        die(f"docker is installed but not answering, so nuke can't tell what runs for this tree; start it or stop it cleanly first ({str(e)[:120]})")
    for row in rows:
        files = row.get("ConfigFiles", "")
        if any(inside(f, tree) for f in files.split(",") if f):
            names.add(row["Name"])
    return names

def must(*args, what):
    """Run a teardown step and stop the whole nuke if it fails, before any
    git happens, so nothing is ever half removed."""
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode != 0:
        die(f"{what} failed, nothing else touched:\n  " + (result.stderr or result.stdout).strip()[:400])

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

    running = []
    procs = processes_for(tree, lock_field(tree, "ports"))
    if procs:
        kill(list(procs))
        left = [pid for pid in procs if pid_alive(pid)]
        if left:
            die(f"process(es) {left} survived SIGKILL; nothing else touched")
        running.append(f"{len(procs)} process(es) killed: " + "; ".join(f"{pid} {why}" for pid, why in sorted(procs.items())))
    projects = compose_projects_under(tree)
    for name in sorted(projects):
        must("docker", "compose", "-p", name, "down", "--volumes", "--remove-orphans", what=f"compose down of {name}")
    if projects:
        running.append("compose down with volumes: " + ", ".join(sorted(projects)))
    mounted, named_volumes = containers_mounting(tree)
    if mounted:
        must("docker", "rm", "-f", "-v", *mounted, what="removing containers " + ", ".join(mounted))
        line = "containers removed: " + ", ".join(mounted)
        if named_volumes:
            must("docker", "volume", "rm", *named_volumes, what="removing volumes " + ", ".join(named_volumes))
            line += ", with named volume(s) " + ", ".join(named_volumes)
        running.append(line)
    stack_line = "; ".join(running) or "nothing running for this worktree"

    lock_line = "no lock in the registry"
    lock = lock_for(tree)
    if lock:
        script = os.path.join(os.path.dirname(__file__), "..", "..", "worktree", "scripts", "worktree_lock.py")
        must(sys.executable, script, "release", "--path", lock["path"], "--status", "released", what="releasing the lock")
        lock_line = "released"

    os.chdir(main_root)
    sh("git", "worktree", "remove", "--force", tree)
    branch_line = f"{branch} kept, it's the main branch"
    if branch not in ("main", "master"):
        result = subprocess.run(["git", "branch", "-D", branch], capture_output=True, text=True)
        if result.returncode == 0:
            branch_line = f"{branch} deleted locally; the remote branch and any PR are untouched"
        else:
            branch_line = f"{branch} NOT deleted: {result.stderr.strip()[:160]}; delete it yourself"

    print(f"running   {stack_line}")
    print(f"worktree  {tree} removed" + (f", {dirty} uncommitted file(s) went with it" if dirty else ""))
    print(f"branch    {branch_line}")
    print(f"lock      {lock_line}")

if __name__ == "__main__":
    main()
