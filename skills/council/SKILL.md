---
name: council
description: "Use for \"council this\", \"score this\", or any artifact judged by taste that the human wants driven to a number: a screen, a doc, copy, a deck. One judge per model provider gets the same artifact, references, and rubric, returns a score per category, and the loop works the weakest category until every judge clears the bar two rounds running. Judges never learn the bar. Code goes to roast and interrogate."
---

# Council

One judge per model provider, same artifact, same references, same rubric, read only. Each judge returns a score per category and the concrete things holding the weakest categories down. The working agent fixes those, captures again, and asks again. The number is the hill. The ledger is the proof.

Anything a program can score, score with the program. Council is for what's left: layout, type, color, copy, structure, tone. The signal comes from provider diversity. Two providers naming the same weakest category is worth acting on. One alone is worth reading.

## Two modes

**Score.** One round, no changes. The human wants to know where the thing stands and what to fix first. Deliver the ledger and the merged findings, then stop.

**Climb.** Rounds until the stop rule holds. The human sets the bar in their message. If they didn't, ask before round one. Don't pick one.

## Step 1. Scope

Write these down before any judge runs and keep them fixed for every round.

- **The kind.** One rubric file from `references/rubrics/`. `ui.md` and `writing.md` ship with the skill. A new kind is a new file in the same shape, nothing else changes.
- **The artifact.** A screen is a named set of screenshots, full resolution, one viewport, the same states every round. Capture with the browser tooling your harness gives you, or take the files the human hands you. Never downscale; a judge once docked half a point for a scaling artifact. A doc or copy is the file itself.
- **References.** Artifacts of the same kind and job that the human would be glad to be compared with. Required for a climb. Judges score them with the same rubric in the same table, so the product's numbers mean something next to them. Without references a judge grading one artifact alone grades kindly and its scores wander a full point between rounds on identical input. Score mode may run without them; the ledger then says the scores are absolute. The human may not have any. Public pages of well-made products are fine, captured in the same theme and viewport as the artifact.
- **Context.** One paragraph: what the product is, who uses it, what this artifact is for. Add the repo's own standard when it has one, a design standard for a screen, `technical-writing` and `voice` for prose, so judges grade against that and not generic taste.
- **Owner calls.** Decisions the human has made that the loop must not touch: the brand bar stays, the table stays a table. They go into the judge prompt as fixed, so judges spend their findings on what can change.

## Step 2. Judges

One per model provider your harness can reach. Spawn each as a read-only subagent with nothing but the prompt file and the files in its context, or run that provider's own command line with the prompt and the files attached. A provider you can't reach is reported as `not available`, never replaced with a second model from a provider already seated. Same-provider judges share blind spots.

Every judge gets `references/judge-prompt.md` filled in: the kind, the context, the artifact list, the reference list, the owner calls, the rubric. Same text to everyone. The prompt never carries the bar, the round number, or earlier scores. A judge that knows the target drifts toward it.

In climb mode, after round one, run only the cheapest judge until it clears the bar, then seat the rest. Mark skipped cells `not run`.

## Step 3. Look before you send

Open the capture yourself before any judge sees it. A clipped column, a control that rendered at the wrong width, a placeholder that didn't show: the judges will score it, and that round is spent on your rendering instead of the design. The same for a doc: read it once as the judges will.

## Step 4. Run a round

All judges at once, in parallel. Keep each judge's table and findings verbatim in the round's folder. Then merge:

1. Per category, each judge's score side by side, the references beside them.
2. Findings two judges raised independently go first.
3. Lone findings stay, marked with who raised them.
4. Disagreements get a line: judge A says X, judge B says X is fine.

## Step 5. The ledger

A table of overall scores, judges by rounds, with the reference scores beside them, and one table per round with every category. If a PR exists, the ledger lives in its description and is edited in place, never posted as a comment. With no PR it goes in the reply.

A judge that gives no score gets `no score`. Never invent one. Never average across judges; the human reads the spread. Note what changed before each round in one line, so a regression can be traced.

## Step 6. Fix (climb mode only)

Work the lowest category per judge first. Write a scope fence before changing anything: which files, which screens. Repeated reviews breed scope creep, and your job is to fight it. Prefer one coherent change set per round over ten unrelated edits; a round that moves ten things and drops a tenth of a point can't say why.

A suggestion you won't take gets a line under `Declined` with the reason. Anything that changes what the artifact does rather than how it looks or reads is a product call: it goes to the human as a Decide, not into the loop. Owner calls are repeated in the ledger every round.

Capture again at the same viewport. Start the next round.

## Step 7. Stop

Climb mode stops when:

- every seated judge scores every category at or above the bar in two consecutive rounds. One round is noise;
- a judge reverses itself on the same element for the second time. Blue, then amber, then neutral for the same status dot is the loop trading one preference for another. That element leaves the loop and goes to the human as a Decide;
- the human says stop.

A round that changes nothing while the judges still disagree on what's weakest means the rubric or the references need work, not more rounds. Bring that to the human.

## Output

**Context.** The paragraph from step 1, with the owner calls.

**Ledger.** The tables from step 5.

**Act on.** The merged findings, weakest category first, each with who raised it. Five at most.

**Disagreements.** One line each.

**Declined** and **Owner calls.** One line each, climb mode only.

**Verdict.** Two sentences. In score mode: what to fix first and whether a climb is worth running. In climb mode: whether the stop rule holds, and which elements are now the human's.

## Where this sits

`roast` is the gate for code and loops one engine until the diff is clean. `interrogate` is one reviewer per model on a diff, findings sorted by the lead, no loop. Council is for what those can't score: a screen, a page, a doc, copy, a deck. Run it on a polish ticket, on something the human says looks or reads wrong, or before a redesign to find out where the thing stands. Not on every PR, and never on code.
