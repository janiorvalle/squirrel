---
name: land-pr
description: "Use every time a change is ready to leave the machine. When the human says commit, push, open a PR, ship it, land it, or finish up, or when a task's last step is getting a change reviewed. Covers branch rules, the gate order, commit titles, PR descriptions, proof, and where it stops."
---

# Land PR

The last stretch of a change. Get it committed, gated, described, proven, and turned in. Then stop. A human merges.

## Branch rules

- Never commit to the default branch. Branch first.
- One task, one branch. Don't pile a second task onto a branch that's already carrying one.
- Push only when the human asked, or when opening the PR is the task. Commit on the task branch as the work goes; the review loop needs a commit per round.

## The gates, in order

`review-by-risk` sets the tier first, from the paths the diff touches. The tier says which of these run. Within the tier, cheap steps that change the code first, the expensive judgment last. Review reads the diff, so if the diff moves after review, you pay for review twice.

1. Format and lint, autofix. Always first.
2. Typecheck or compile.
3. Fast tests. Seconds to a couple of minutes.
4. Shrink the diff. Read it against the ticket's Fix and Done-when lines and remove what they didn't ask for. `no-comments` runs here.
5. Low tier: a fresh-context read. A subagent that didn't write the code gets the diff and the ticket, nothing else, and reports. Verify each finding against the code and fix what holds inside the diff; anything else is a follow-up. A verified finding moves the change to standard.
6. Standard and high: the roast loop below.
7. High only: `interrogate`, then `blast-radius`, then the approver named in the PR, from REVIEW.md's High risk line or the human directing the task.
8. The full suite, once, against the final diff. Every tier.

If the full suite fails at the end, don't restart the review loop. Fix it, rerun the fast tier, review only the fix.

Use the project's own commands for each step. This is about the order, not the tools.

## The roast loop

Freeze the fence first, the way the roast skill says: the request in a sentence, the intended behavior, the owner boundary, the files, and the diff's size at the freeze, files and non-test lines, in a file outside the repo. Pass it every round with `--intent-file`, and the repo's REVIEW.md with `--context-glob REVIEW.md` when it has one. Beyond those, the verdict file, and the prior verdicts the loop names below, no other flags, and never `--max-priority`; the threshold is the human's and the default is theirs.

1. Commit the work. Round one reads the whole branch: `roast --base origin/<base> --json-output <round-1.json>`, the verdict file outside the repo like the intent file.
2. Every finding is a claim. Verify it against the real code.
3. Sort each verified finding. These are the only outcomes:
   - Caused by this diff, inside the fence, wherever it shows, an untouched caller the diff broke included: fix it.
   - Not caused by this diff: pre-existing. Roast tags it from git blame; below P0 it's an observation that never makes the verdict raw, so it's one sentence in the report, never a fix. A pre-existing P0, one of the four things below that widen the fence, is fixed or shown wrong.
   - Outside the fence: a follow-up. Resolve it with that reason.
   - Wrong: resolve it with the evidence.
   - Needs a new contract, a migration, a different owner, or a design call: stop and bring it as a Decide. The only thing that stops the loop mid-task.
   A finding this diff caused inside the fence is fixed or escalated. "Later" and "too hard" are not reasons.
4. Annotate the round's verdict file: on each finding you set aside, add a `resolution` with a `kind` of `pre-existing`, `out-of-fence`, or `wrong` and a one-sentence `reason`. Roast refuses any kind but `wrong` on a P0. The intent file stays the fence and holds no resolutions.
5. Rerun gates one to three on the fix, then `roast --dirty` with the same flags, `--prior-verdict <file>` once per annotated verdict so far, and a new `--json-output`. It reads only the uncommitted fix, and a finding that recurs with the same file and title comes back resolved, printed in its own block, never raw. Commit the fix once read.
6. Repeat until roast says well done, or every open finding is resolved with a reason. There is no round cap. The fence is the cap.

The fence holds the diff. When the diff grows past double the files or non-test lines recorded at the freeze, that's drift: revert the edits outside the fence and keep going on what's left. Only active data loss, a crash, a broken build or install, or a concrete security exposure may widen the fence, and those four are the only pre-existing findings that get fixed.

## Commit titles

For someone scanning history months from now. Conventional commit style. Name the outcome, not the mechanism. The mechanism is in the diff.

Bad: `perf(server): negotiate permessage-deflate on the websocket`
Good: `perf(server): cut websocket frame size by 70% with gzipping`

## The PR description

For a reviewer deciding what this change means for the product. Open with the problem in a sentence or two, then how you solved it. Plain words. No file-by-file tour.

Bad:

> Removed implicit workspace carry-over from every "new thread" entry point (cmd+n, sidebar buttons, command palette). New threads inherit only the project from context. Deleted buildContextualThreadOptions and the v1 sidebar's seed-context machinery.

Good:

> My "new worktree" default was ignored when starting new threads on existing worktrees. Super unintuitive. Now your preferences always apply.

Write it in the ticket shape from `tracker`: problem, fix, done when. Put the task id in it. Then three lines a human reads instead of the diff:

```
Risk: the tier, and what breaks if this is wrong, in one line.
Revert: the one command or step that undoes it.
Review: each finding resolved with a reason, one line each, or none.
```

A high-tier PR also names its human approver, from REVIEW.md's High risk line or the human directing the task. End with a line naming the model and harness that made the change.

## Proof

Don't say it works, show it. Anything a user can see gets screenshots, including empty, loading, and error states. Anything that changes state gets receipts, the request and the stored result. Bug fixes show the broken state first, then the same capture fixed. Web flows get a keyboard-only pass.

All of it goes on the ticket at turn-in, the way `tracker` says for the repo's backend, never into git.

## After the PR is open

Check CI before calling it done. A red check is never "probably fine". Read the failing log to the real error.

## Where it stops

Turn the task in with the PR open, evidence attached, and the branch left standing. Then stop. A human reviews and merges. The task gets completed after the merge, and not by the agent that built it.

## Never

- Merge your own PR unless the human explicitly said to.
- Change the diff after review without rerunning the fast tier and reviewing the delta.
- Open a PR with failing or unread checks and call it done.
- Skip the tier's gate quietly. If roast isn't installed, say so and point at tools.md.
- Fix a finding this diff didn't cause, short of data loss, a crash, a broken build, or a security exposure. It's pre-existing; resolve it with that reason.
- Stop the loop because it's still finding things. Only a design call stops it.
