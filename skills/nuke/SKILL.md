---
name: nuke
description: "Use only when the human types /nuke. Tears down one worktree and stops: whatever runs for it, dev servers, compose stacks, containers, then its lock, the tree, the local branch. Refuses if the branch has commits the remote doesn't. The undo of worktree."
disable-model-invocation: true
---

# Nuke

The human's way to throw a task's workspace away in one word. You never reach for this on your own; the human typing `/nuke` is the approval the `worktree` skill asks for before containers and volumes go.

## What it does

The script sits beside this file, so run it by that path from wherever you are, naming the worktree or standing in it:

```
python3 <this skill's folder>/scripts/nuke.py [worktree-path]
```

In order:

1. Everything running for this tree, found three ways so it doesn't matter whether the task used docker, a dev server, both, or neither: processes whose working directory is inside the tree or that listen on the ports the lock recorded, killed; compose projects whose files live inside the tree, `down` with orphans, then the volumes compose named after the project, never one a copied `.env` merely names, and never a volume with a name of its own in the file, since that can be another checkout's database too; plain containers with a bind mount from inside the tree or a lock port published, removed with their named volumes, except a volume another container still uses or a compose project owns, which is kept and named in the report. Never anything from another tree, so the shared checkout's stack, servers, and data are safe, and never the shell or agent that ran the command.
2. The lock, released in the registry.
3. The worktree, removed with force. Uncommitted files go with it; that's the point.
4. The local branch, deleted. The remote branch and any PR stay, since deleting reviewable work is a different decision.

Then it prints four lines, running, worktree, branch, lock, and you stop. No next step, no offer.

## The one refusal

If the branch has commits the remote doesn't, nothing is touched and the script says which commits. Push them or delete them yourself, then run it again. That's the only case nuke can't undo.

It also refuses to run on the main checkout, since that isn't a worktree, and on Windows, where it can't see processes the way it needs to; there the teardown is by hand. It fetches every remote before it trusts them, so a branch someone deleted or force-pushed upstream since your last fetch still counts as unpushed, and a push without `-u` still counts as pushed, since what matters is that some remote branch holds the commit, not which branch the upstream is. A process in the tree that belongs to another user stops it before anything is signalled, since it can't stop that one and won't stop the rest without it. A docker daemon on the machine with no docker command on the PATH stops it too, rather than letting containers hide, and so does a docker context or `DOCKER_HOST` that points at another machine, since nothing there is this tree's. A port the lock recorded that something outside the tree holds stops it too, since that means the lock is stale, not that the stranger is yours. It looks at everything before it touches anything, so a docker daemon that isn't answering or a missing tool stops the run with nothing changed. Once teardown has begun, a step that fails stops the run there, and the message says what was already done, so the report is true even when the run is cut short.

## Never

- Run it because you think the work is done. Only the human runs it.
- Widen it to a remote branch, a PR, or another tree's stack.
- Keep going after the report. Hold.
