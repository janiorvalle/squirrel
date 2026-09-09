#!/usr/bin/env python3
"""Read and write the squirrel worktree registry with a file lock.

Usage:
  worktree_lock.py list [--status active] [--repo name]
  worktree_lock.py claim --repo R --path P --branch B --base-ref REF --owner O --purpose TEXT [--ports k=v,k=v] [--host H]
  worktree_lock.py heartbeat --path P [--notes TEXT] [--ports k=v]
  worktree_lock.py release --path P --status released|handoff|stale

Registry: $SQUIRREL_WORKTREE_REGISTRY or ~/.config/squirrel/worktree-locks.json
"""
import argparse, json, os, sys
from datetime import datetime, timezone

if os.name == "nt":
    import msvcrt

    # Windows locks a byte range, not a file. The first byte is the whole registry's lock,
    # and the range may sit past the end of a registry that is still empty.
    def lock(f):
        f.seek(0); msvcrt.locking(f.fileno(), msvcrt.LK_LOCK, 1)

    def unlock(f):
        f.seek(0); msvcrt.locking(f.fileno(), msvcrt.LK_UNLCK, 1)
else:
    import fcntl

    def lock(f):
        fcntl.flock(f, fcntl.LOCK_EX)

    def unlock(f):
        fcntl.flock(f, fcntl.LOCK_UN)

REGISTRY = os.environ.get("SQUIRREL_WORKTREE_REGISTRY") or os.path.expanduser("~/.config/squirrel/worktree-locks.json")

def now():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

def parse_ports(text):
    if not text:
        return {}
    out = {}
    for pair in text.split(","):
        k, _, v = pair.partition("=")
        out[k.strip()] = int(v) if v.strip().isdigit() else v.strip()
    return out

class Registry:
    def __enter__(self):
        os.makedirs(os.path.dirname(REGISTRY), exist_ok=True)
        self.f = open(REGISTRY, "a+")
        lock(self.f)
        self.f.seek(0)
        raw = self.f.read()
        self.data = json.loads(raw) if raw.strip() else {"locks": []}
        return self
    def save(self):
        self.f.seek(0); self.f.truncate()
        json.dump(self.data, self.f, indent=2); self.f.write("\n")
    def __exit__(self, *a):
        unlock(self.f); self.f.close()
    def find(self, path):
        return next((l for l in self.data["locks"] if l["path"] == path), None)

def cmd_list(a):
    with Registry() as r:
        rows = [l for l in r.data["locks"] if (not a.status or l["status"] == a.status) and (not a.repo or l["repo"] == a.repo)]
    print(json.dumps(rows, indent=2))

def cmd_claim(a):
    with Registry() as r:
        existing = r.find(a.path)
        if existing and existing["status"] == "active" and existing["owner"] != a.owner:
            sys.exit(f"ERROR locked: {a.path} is active and owned by {existing['owner']}. Pick another path or ask the human to hand it over.")
        lock = existing or {}
        lock.update({
            "repo": a.repo, "host": a.host, "path": a.path, "branch": a.branch, "base_ref": a.base_ref,
            "owner": a.owner, "purpose": a.purpose, "status": "active",
            "ports": parse_ports(a.ports) or lock.get("ports", {}),
            "created_at": lock.get("created_at") or now(), "updated_at": now(), "notes": a.notes or lock.get("notes", ""),
        })
        if not existing:
            r.data["locks"].append(lock)
        r.save()
    print(f"claimed {a.path} for {a.owner}")

def cmd_heartbeat(a):
    with Registry() as r:
        lock = r.find(a.path) or sys.exit(f"ERROR no lock for {a.path}. Claim it first.")
        lock["updated_at"] = now()
        if a.notes: lock["notes"] = a.notes
        if a.ports: lock["ports"] = {**lock.get("ports", {}), **parse_ports(a.ports)}
        r.save()
    print(f"heartbeat {a.path}")

def cmd_release(a):
    with Registry() as r:
        lock = r.find(a.path) or sys.exit(f"ERROR no lock for {a.path}.")
        lock["status"] = a.status; lock["updated_at"] = now()
        if a.notes: lock["notes"] = a.notes
        r.save()
    print(f"{a.status} {a.path}")

p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
sub = p.add_subparsers(dest="cmd", required=True)
s = sub.add_parser("list"); s.add_argument("--status"); s.add_argument("--repo"); s.set_defaults(fn=cmd_list)
s = sub.add_parser("claim")
for name in ("--repo", "--path", "--branch", "--base-ref", "--owner", "--purpose"): s.add_argument(name, required=True)
s.add_argument("--host", default="local"); s.add_argument("--ports"); s.add_argument("--notes"); s.set_defaults(fn=cmd_claim)
s = sub.add_parser("heartbeat"); s.add_argument("--path", required=True); s.add_argument("--notes"); s.add_argument("--ports"); s.set_defaults(fn=cmd_heartbeat)
s = sub.add_parser("release"); s.add_argument("--path", required=True); s.add_argument("--status", required=True, choices=["released", "handoff", "stale"]); s.add_argument("--notes"); s.set_defaults(fn=cmd_release)
args = p.parse_args(); args.fn(args)
