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

def common_git_dir(tree):
    """The repository every worktree of this tree shares: the main checkout's
    .git, or the bare repository itself. Git commands that outlive the tree
    run against it."""
    return os.path.realpath(os.path.join(tree, sh("git", "rev-parse", "--git-common-dir", cwd=tree)))

def is_main_worktree(tree):
    return os.path.realpath(os.path.join(tree, sh("git", "rev-parse", "--git-dir", cwd=tree))) == common_git_dir(tree)

def unpushed(tree):
    """Commits no remote branch has. Every remote is fetched first, so what
    was deleted or force-pushed since the last fetch counts. When some remote
    branch contains HEAD the work is safe, whichever branch the upstream
    happens to be; otherwise the commits past the upstream, or past the
    lock's base ref, are the ones at risk."""
    if not sh("git", "remote", cwd=tree):
        die("this repository has no remote, so nothing holds the branch's commits but this disk; nuke won't delete them")
    if subprocess.run(["git", "fetch", "--all", "--quiet", "--prune"], cwd=tree, capture_output=True).returncode != 0:
        die("can't reach the remote to check what's pushed; nothing touched")
    holders = [ref.strip() for ref in sh("git", "branch", "-r", "--contains", "HEAD", cwd=tree).splitlines() if "->" not in ref]
    if holders:
        return [], holders[0]
    try:
        against = sh("git", "rev-parse", "--abbrev-ref", "@{upstream}", cwd=tree)
    except subprocess.CalledProcessError:
        against = lock_field(tree, "base_ref")
        if not against:
            die("no remote branch has this commit, the branch has no upstream, and no lock records its base, so nuke can't say what's pushed; push the branch first")
    try:
        return sh("git", "log", "--oneline", f"{against}..HEAD", cwd=tree).splitlines(), against
    except subprocess.CalledProcessError:
        die(f"no remote branch has this commit and {against} is gone from the remote; push the branch again or delete it yourself first")

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

def ancestors():
    """This process and every process above it: the shell, the agent, the
    harness. Nuke never kills the chain that invoked it."""
    chain, pid = set(), os.getpid()
    while pid and pid not in chain:
        chain.add(pid)
        try: pid = int(sh("ps", "-o", "ppid=", "-p", str(pid), check=False).strip() or 0)
        except ValueError: break
    return chain

def inside(path, tree):
    """True for the tree itself and anything under it, by real path."""
    path, tree = os.path.realpath(path), os.path.realpath(tree)
    return path == tree or path.startswith(tree + os.sep)

def processes_for(tree, ports):
    """Processes whose working directory is inside the tree, plus whatever is
    listening on the ports the lock recorded: dev servers, watchers, shells an
    agent left behind. Never this script's own process tree. Ports docker
    holds come back separately, for the container side to match."""
    mine = ancestors()
    cwds = working_directories()
    found = {pid: f"cwd {cwd}" for pid, cwd in cwds.items() if inside(cwd, tree) and pid not in mine}
    docker_ports = {}
    for name, port in (ports or {}).items():
        for pid in listeners(port):
            if pid in mine or pid in found: continue
            if inside(cwds.get(pid, ""), tree):
                found[pid] = f"listening on {name} port {port}"
            elif container_publishing(port):
                docker_ports[str(port)] = name  # the container side finds which container, and whose
            else:
                die(f"port {port} ({name} in the lock) is held by pid {pid} {process_name(pid)}, whose working directory isn't in this tree, so the lock's port looks stale; stop it yourself or fix the lock first. Nothing touched")
    return found, docker_ports

def working_directories():
    """Every visible process and its working directory."""
    cwds = {}
    if shutil.which("lsof"):
        for line in sh("lsof", "-nP", "-d", "cwd", "-Fpn", check=False).splitlines():
            if line.startswith("p"): pid = int(line[1:])
            elif line.startswith("n"): cwds[pid] = line[1:]
    elif os.path.isdir("/proc"):
        for entry in os.listdir("/proc"):
            if not entry.isdigit(): continue
            try: cwds[int(entry)] = os.readlink(f"/proc/{entry}/cwd")
            except OSError: continue
    else:
        die("neither lsof nor /proc is available, so nuke can't see what runs in this tree; install lsof first")
    return cwds

def listeners(port):
    if shutil.which("lsof"):
        return [int(p) for p in sh("lsof", "-nP", "-t", f"-iTCP:{port}", "-sTCP:LISTEN", check=False).split()]
    if shutil.which("ss"):
        return [int(p) for p in re.findall(r"pid=(\d+)", sh("ss", "-ltnp", f"sport = :{port}", check=False))]
    die(f"neither lsof nor ss is available, so nuke can't see who listens on port {port}")

def process_name(pid):
    return sh("ps", "-o", "comm=", "-p", str(pid), check=False)

def container_publishing(port):
    """Whether some container publishes this host port. The listener itself
    is docker's or OrbStack's helper, whose name says nothing."""
    return docker_present() and bool(sh("docker", "ps", "-q", "--filter", f"publish={port}", check=False))

def pid_alive(pid):
    try: os.kill(pid, 0); return True
    except ProcessLookupError: return False
    except PermissionError: return True

def kill(pids):
    import signal, time
    for pid in pids:
        try: os.kill(pid, 0)
        except PermissionError: die(f"pid {pid} runs in this tree as another user, so nuke can't stop it; stop it yourself first. Nothing touched")
        except ProcessLookupError: pass
    for pid in pids:
        try: os.kill(pid, signal.SIGTERM)
        except ProcessLookupError: pass
    time.sleep(1)
    for pid in pids:
        try: os.kill(pid, signal.SIGKILL)
        except ProcessLookupError: pass

def docker_present():
    """True when the docker command is here and talks to this machine. A
    daemon socket or DOCKER_HOST with no command means containers may run
    unseen, and a remote endpoint means the containers aren't this tree's,
    so both stop the run."""
    if shutil.which("docker"):
        endpoint = os.environ.get("DOCKER_HOST") or sh("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}", check=False)
        if not endpoint.startswith(("unix://", "npipe://")):
            where = endpoint or "an endpoint nuke can't read"
            die(f"docker points at {where}, not this machine, so its containers can't be this tree's; switch to the local context first. Nothing touched")
        return True
    sockets = ["/var/run/docker.sock", os.path.expanduser("~/.docker/run/docker.sock")]
    if os.environ.get("DOCKER_HOST") or any(os.path.exists(p) for p in sockets):
        die("a docker daemon is here but the docker command isn't on PATH, so nuke can't see the tree's containers; put docker on PATH first. Nothing touched")
    return False

def labels_of(container):
    return (container.get("Config") or {}).get("Labels") or {}

def publishes(container, host_ports):
    bindings = (container.get("NetworkSettings") or {}).get("Ports") or {}
    return any(b.get("HostPort") in host_ports for binds in bindings.values() for b in binds or [])

def containers_mounting(tree, docker_ports, projects):
    """Plain containers, compose or not, with a bind mount from inside the
    tree or a lock port published, and the named volumes they use, which
    `docker rm -v` leaves behind. Compose containers are their project's;
    one publishing a lock port from a project outside the tree means the
    lock is stale, and that stops the run."""
    names, volumes = [], []
    if not docker_present():
        if docker_ports: die(f"docker holds lock port(s) {', '.join(docker_ports)} but isn't on PATH; nothing touched")
        return names, [], []
    listing = subprocess.run(["docker", "ps", "-aq"], capture_output=True, text=True)
    if listing.returncode != 0:
        die(f"docker is installed but not answering; nuke can't list containers ({listing.stderr.strip()[:120]})")
    ids = listing.stdout.split()
    if not ids: return names, [], []
    try:
        containers = json.loads(sh("docker", "inspect", *ids))
    except (subprocess.CalledProcessError, json.JSONDecodeError) as e:
        die(f"docker inspect failed; nuke can't tell which containers use this tree ({str(e)[:120]})")
    for c in containers:
        mounts = c.get("Mounts", [])
        project = labels_of(c).get("com.docker.compose.project")
        on_port = publishes(c, docker_ports)
        if project and on_port and project not in projects:
            die(f"container {c['Name'].lstrip('/')} of compose project {project}, which isn't in this tree, publishes a lock port ({', '.join(docker_ports)}), so the lock looks stale; fix it first. Nothing touched")
        if project:
            continue  # compose owns it; its project's down takes it
        if on_port or any(inside(m.get("Source", ""), tree) for m in mounts if m.get("Type") == "bind"):
            names.append(c["Name"].lstrip("/"))
            volumes += [m["Name"] for m in mounts if m.get("Type") == "volume" and m.get("Name")]
    if docker_ports and not any(publishes(c, docker_ports) for c in containers):
        die(f"docker holds lock port(s) {', '.join(docker_ports)} but no container publishes them, so the lock looks stale; fix it first. Nothing touched")
    ours = set(c["Id"] for c in containers if c["Name"].lstrip("/") in names)
    removable, kept = [], []
    for volume in sorted(set(volumes)):
        users = [u for u in sh("docker", "ps", "-aq", "--filter", f"volume={volume}", check=False).split() if not any(full.startswith(u) for full in ours)]
        owner = sh("docker", "volume", "inspect", volume, "--format", "{{index .Labels \"com.docker.compose.project\"}}", check=False)
        if users or owner:
            kept.append(f"{volume} ({'used by ' + str(len(users)) + ' other container(s)' if users else 'owned by compose project ' + owner})")
        else:
            removable.append(volume)
    return names, removable, kept

def compose_projects_under(tree):
    """Compose projects whose config files live inside this worktree. Only
    that: a project name in the tree's .env proves nothing, since a copied
    .env can name the main checkout's project."""
    names = {}
    if not docker_present():
        return names
    try:
        rows = json.loads(sh("docker", "compose", "ls", "--all", "--format", "json") or "[]")
    except (subprocess.CalledProcessError, json.JSONDecodeError) as e:
        die(f"docker is installed but not answering, so nuke can't tell what runs for this tree; start it or stop it cleanly first ({str(e)[:120]})")
    for row in rows:
        files = [f for f in row.get("ConfigFiles", "").split(",") if f]
        if any(inside(f, tree) for f in files):
            names[row["Name"]] = files
    return names

def project_volumes(project):
    """After a project is down, its volumes: the ones compose named after the
    project are its own; one with a name of its own (`name:` in the file) can
    be another checkout's database too, so it stays, and so does any volume
    a container still uses."""
    own, shared = [], []
    for volume in sh("docker", "volume", "ls", "-q", "--filter", f"label=com.docker.compose.project={project}", check=False).split():
        in_use = sh("docker", "ps", "-aq", "--filter", f"volume={volume}", check=False).split()
        if volume.startswith(project + "_") and not in_use:
            own.append(volume)
        else:
            shared.append(volume)
    return own, shared

def must(*args, what, done=()):
    """Run a teardown step; if it fails, stop and say what already happened,
    so the report is true even when the run is cut short."""
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode != 0:
        so_far = ("; done before it: " + "; ".join(done)) if done else "; nothing was changed before it"
        die(f"{what} failed{so_far}\n  " + (result.stderr or result.stdout).strip()[:400])

def main():
    if os.name == "nt":
        die("nuke runs on macOS and Linux only; on Windows, stop the stack, run `git worktree remove --force`, and release the lock by hand")
    start = os.path.abspath(sys.argv[1]) if len(sys.argv) > 1 else os.getcwd()
    tree = worktree_root(start)
    if is_main_worktree(tree):
        die(f"{tree} is the main checkout, not a worktree; nuke only removes worktrees")
    git = ["git", f"--git-dir={common_git_dir(tree)}"]
    branch = sh("git", "rev-parse", "--abbrev-ref", "HEAD", cwd=tree)
    ahead, against = unpushed(tree)
    if ahead:
        die(f"{branch} has {len(ahead)} commit(s) the remote doesn't ({against}); push them or delete them yourself first:\n  " + "\n  ".join(ahead))
    dirty = len(sh("git", "status", "--porcelain", cwd=tree).splitlines())

    # Look at everything before touching anything, so a docker daemon that
    # isn't answering or a missing tool stops the run with nothing changed.
    procs, docker_ports = processes_for(tree, lock_field(tree, "ports"))
    projects = compose_projects_under(tree)
    mounted, named_volumes, kept_volumes = containers_mounting(tree, docker_ports, projects)

    running = []
    if procs:
        kill(list(procs))
        left = [pid for pid in procs if pid_alive(pid)]
        if left:
            die(f"process(es) {left} survived SIGKILL; the others were killed, nothing else changed")
        running.append(f"{len(procs)} process(es) killed: " + "; ".join(f"{pid} {why}" for pid, why in sorted(procs.items())))
    for name, files in sorted(projects.items()):
        flags = [flag for f in files for flag in ("-f", f)]
        must("docker", "compose", "-p", name, *flags, "down", "--remove-orphans", what=f"compose down of {name}", done=running)
        line = f"compose down: {name}"
        own, shared = project_volumes(name)
        if own:
            must("docker", "volume", "rm", *own, what=f"removing volumes of {name}", done=running + [line])
            line += ", volumes removed: " + ", ".join(own)
        if shared:
            line += "; kept, named for more than this project: " + ", ".join(shared)
        running.append(line)
    if mounted:
        must("docker", "rm", "-f", "-v", *mounted, what="removing containers " + ", ".join(mounted), done=running)
        line = "containers removed: " + ", ".join(mounted)
        if named_volumes:
            must("docker", "volume", "rm", *named_volumes, what="removing volumes " + ", ".join(named_volumes), done=running + [line])
            line += ", with named volume(s) " + ", ".join(named_volumes)
        if kept_volumes:
            line += "; kept, not only this tree's: " + ", ".join(kept_volumes)
        running.append(line)
    stack_line = "; ".join(running) or "nothing running for this worktree"

    os.chdir(os.path.dirname(tree))
    must(*git, "worktree", "remove", "--force", tree, what="removing the worktree", done=running)

    lock_line = "no lock in the registry"
    lock = lock_for(tree)
    if lock:
        script = os.path.join(os.path.dirname(__file__), "..", "..", "worktree", "scripts", "worktree_lock.py")
        must(sys.executable, script, "release", "--path", lock["path"], "--status", "released", what="releasing the lock", done=running + ["worktree removed"])
        lock_line = "released"
    branch_line = f"{branch} kept, it's the main branch"
    if branch not in ("main", "master"):
        result = subprocess.run([*git, "branch", "-D", branch], capture_output=True, text=True)
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
