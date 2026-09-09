---
name: nuke
description: "Use only when the human types /nuke. Tears down one worktree and stops: its compose stack with volumes, its lock, the tree, the local branch. Refuses if the branch has commits the remote doesn't. The undo of worktree."
disable-model-invocation: true
---

# Nuke

The human's way to throw a task's workspace away in one word. You never reach for this on your own; the human typing `/nuke` is the approval the `worktree` skill asks for before containers and volumes go.

## What it does

Run the script from the worktree to nuke, or name it:

```
python3 skills/nuke/scripts/nuke.py [worktree-path]
```

In order:

1. The compose stack, `down` with volumes and orphans, for every compose project whose files live inside this worktree or that the worktree's `.env` names. Never a project from another tree, so the shared checkout's stack is safe.
2. The lock, released in the registry.
3. The worktree, removed with force. Uncommitted files go with it; that's the point.
4. The local branch, deleted. The remote branch and any PR stay, since deleting reviewable work is a different decision.

Then it prints four lines, stack, worktree, branch, lock, and you stop. No next step, no offer.

## The one refusal

If the branch has commits the remote doesn't, nothing is touched and the script says which commits. Push them or delete them yourself, then run it again. That's the only case nuke can't undo.

It also refuses to run on the main checkout, since that isn't a worktree.

## Never

- Run it because you think the work is done. Only the human runs it.
- Widen it to a remote branch, a PR, or another tree's stack.
- Keep going after the report. Hold.
