---
name: review-by-risk
description: "Use before the gates on any change, to decide how much review it gets. The paths a diff touches set its tier, low, standard, or high, and the tier picks the gate: a fresh-context read, the roast loop, or the loop plus interrogate and blast-radius with a named human. Also owns REVIEW.md, the file where a repo tells the reviewer what matters, what to ignore, which paths are high risk, and which noise to drop."
---

# Review by risk

A copy fix and an auth change don't deserve the same review. The change picks its tier by the paths it touches, never by who wrote it. The tier picks the gate. Start strict. A path moves down only when the regression log shows it has stayed quiet, and only the human moves it; an agent proposes the move in a PR with the evidence.

## The tiers

The highest tier of any touched file wins.

| Tier | Paths | Gate |
|---|---|---|
| Low | Docs, copy, comments, tests with no production code, and paths REVIEW.md lists under Low risk | Format and lint, typecheck, fast tests, shrink the diff, one fresh-context read, the full suite |
| Standard | Everything else | The same, with the roast loop from `land-pr` in place of the fresh read |
| High | Auth, billing, payments, CI workflow files, installers, release and deploy code, migrations, and paths REVIEW.md lists under High risk | The standard gate, then `interrogate` and `blast-radius`, and the approver named in the PR |

The fresh-context read: a subagent that didn't write the code gets the diff and the ticket, nothing else, and reports what's wrong. Its findings are claims. Verify each against the real code, fix what holds inside the diff, and record the rest as follow-ups.

A low-tier change with a verified finding from the fresh read moves up to standard. Low means review was cheap, never that it was skipped.

A high-tier change names a human approver in the PR, on top of the human who merges: the one on the path's High risk line in REVIEW.md, or the human directing the task when the line names none. An agent's approval never counts as a human's.

## REVIEW.md

One per repo, at the root, short. It's where the repo tells the reviewer what matters here. Roast reads it when `land-pr` passes `--context-glob REVIEW.md`, every round.

```
# Review

## What matters
The config must never route a tenant to another tenant's database.
Docs describe the current flags; a stale flag name in docs is a bug.

## Ignore
Generated files under src/gen/.
Fixtures under tests/fixtures/.

## High risk
src/auth/, approver @maria
.github/workflows/

## Low risk
docs/
content/

## Known noise
Repository rows are never null, the ORM guarantees it. Don't flag missing checks on them.
```

Every section is optional. Each line is a claim about the repo, so write it the way the reviewer can check it: the file, the guarantee, and where the guarantee comes from.

The rule for filling it. The second time roast flags the same thing that isn't a bug, it gets a line under Known noise. The second time it flags the same real bug class, that becomes a lint or a test, never a line here. `dont-say-it-twice`. Moving a path between High risk and Low risk is the human's call, so an agent proposes it in the PR and never edits those two sections on its own.

## The test

Can you say from the diff alone what review it gets and why? If the answer depends on who wrote it or how risky it felt, the tier isn't set.
