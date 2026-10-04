---
name: mockup
description: "Use for \"mock this up\", \"show me what this would look like\", \"prototype this flow\", or before building any UI where the shape isn't settled. One self-contained HTML file with a tab per state of the flow, so a person clicks through the whole thing with no backend and circles what's wrong for you to read back. This is how design-it-twice and experience-first prototype."
---

# Mockup

One HTML file, no dependencies, a tab bar across the top, one tab per state of the flow. Someone opens it and clicks through the feature end to end before anyone writes real code. Then they switch on Feedback, circle what's wrong, and save the notes for you to read. Cheap to make, cheap to throw away, and it settles arguments that descriptions can't.

## What you're given

Some of: a description of the feature and its flow, a screenshot of the app it lives in, a design system to match, the specific states they want to see.

## 1. Work out the states

From the description, list every distinct screen or state. The usual shapes:

- Create or edit: empty, form, saving, saved, error.
- Upload: pick a file, uploading, preview, confirm, done.
- Wizard: step one, step two, review, submit, confirmation.
- Dashboard: loading, populated, empty, filtered, detail.
- Action: idle, in progress, complete, failed.

Show the list before building. "I see five states, input, processing, preview, request changes, created. Adjust anything?" Wait for a yes.

## 2. Pick the look

In this order:

1. **A screenshot.** Match it exactly. Colors, type, spacing, form controls, buttons, section headers, layout. Reproduce the chrome, nav, sidebar, header, footer, so the feature shows up in context. Study it before writing a line.
2. **A design system.** Its tokens, components, patterns.
3. **An app name.** Approximate that app's look.
4. **Nothing.** Clean default. White or light gray background, dark text, the system font stack, subtle borders and shadows, one blue action color, 14px base. No framework.

## 3. Build the file

A sticky dark bar at the top, `#2c2c2c`, "Flow state:" on the left in a muted color, then one numbered tab per state, "1. Input", "2. Processing". The active tab is lighter. Clicking a tab shows that state and scrolls to the top.

The feedback layer reads three attributes, so the bar and the states follow this skeleton:

```html
<nav id="flow-bar">
  <span>Flow state:</span>
  <button data-tab="1">1. Input</button>
  <button data-tab="2">2. Processing</button>
</nav>
<section data-state="1">...</section>
<section data-state="2" hidden>...</section>
```

Give the bar `display: flex`, so the Feedback switch sits at its right end. Show one state at a time and hide the rest with `hidden` or `display: none`. Let the page itself scroll, never a container inside a state, because the layer places boxes against the page.

Each state is a full page, not a fragment. The whole chrome repeated, then the feature area in that state, with realistic data and the right feedback: a success banner, an error message, a spinner.

Interaction stays small. Tab switching. Hover on buttons. Action buttons that jump to the logical next tab, submit goes to processing. Collapsible sections where they make sense. Form fields present but not validated.

Paste `references/feedback.html` from this skill's folder just before `</body>`, whole and unchanged. It adds the Feedback switch to the bar and everything the reviewer marks up with. Never edit it per mockup, and never restyle it to match the look.

Nothing external. No CDN, no framework, no fetch, no build step. Only CSS transitions and simple spinners.

## 4. Realistic data

Never lorem ipsum. Names that fit the domain. Plausible numbers and dates. The status labels the app would actually use. Three to six varied rows in any table. Real error text. Empty states that say what to do next.

## 5. Hand it over

Save it somewhere the human can find it, open it in the browser, tell them it's open and list the tabs. Offer to save it somewhere permanent if they want to keep it.

Tell them how to give feedback, in these words or close to them:

> To mark something up, press Feedback in the bar. Drag a box over anything that's wrong and type what should change. Use Note on this screen for anything that isn't one spot, and tick Applies everywhere when it holds for every screen. When you're done, press Save notes and tell me. It downloads `<name>.notes.json` to your downloads folder. Notes also stay in this browser for this file, so a reload keeps them, but they don't travel with the file.

Changes go into the same file. Reopen, say what moved.

## 6. Reading the feedback

Read the notes when the human says they saved them.

1. Open the newest `<name>.notes.json` in their downloads folder, usually `~/Downloads`. `<name>` is the mockup's file name without `.html`. The browser names a repeat download `<name>.notes (1).json`, so sort by time. If they pasted the notes into the chat instead, read them from the message.
2. Read `tabs` for the mockup's tabs and `notes` for the notes, in number order.
3. Make each note's change in the mockup file, then reply.

Each note holds:

- `number`, the number the reviewer saw on the box and in the list.
- `tab`, the tab's `number` and `name`. The note is about the `data-state` section with that number.
- `box`, the box in page pixels from the top left corner of that state, as `x`, `y`, `width`, and `height`. It's `null` for a note on the whole screen.
- `element`, what the box covers, read from the page when the box was drawn. `tag`, `role`, and `text` name it. `heading` is the nearest heading above it. `path` is the last four steps of its CSS path inside the state. When the box spans several elements, `element` is their nearest shared parent and `covers` lists each one with text. It's `null` for a note on the whole screen.
- `everywhere`, `true` when the reviewer ticked Applies everywhere.
- `text`, what the reviewer typed.
- `created`, when the note was made.

Find the element by its `text` and `heading` in that tab's section. Use `path` and `box` only to break a tie. Change that element and nothing near it. A note with `everywhere: true` is a change to every tab where the same thing shows up, not only the tab it was drawn on. A box with empty `text` names a spot but not a problem, so ask what's wrong there instead of guessing.

Reply with one line per note, by number. "3. Moved Send invites to the right of Cancel on every tab." A note you didn't act on gets its line too, with the reason. Then tell the human to reload, and that old notes stay in their browser until they delete them in the list.

## Before you hand it over

Every state has a complete tab. The bar works. Feedback switches on and off, and with it off the mockup behaves as if the layer weren't there. The look matches the reference. The data reads as real. Action buttons cross-link to the next state. No external dependencies. It holds up at common widths. Loading states spin. Success states confirm. Error states explain.

## Some flows for reference

- Checkout: cart, shipping, payment, processing, confirmation, error.
- Import: upload, validating, preview, confirm, success, partial failure.
- Settings: view, edit, saving, saved.
- Search: initial, loading, results, no results, detail.
- Approval: draft, submitted, under review, approved, rejected.
