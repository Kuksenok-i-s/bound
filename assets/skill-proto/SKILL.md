---
name: proto
description: Token-frugal HTML prototyping for pre-production research (hypothesis pages, business-logic mockups, clickable flows). Use whenever the user asks for an HTML page, mockup, prototype, dashboard, flow or screen using the team's own design system and sample data. Enforces spec → reusable primitives → assembly by a cheaper model → human review loop. Do not use for production frontend components.
---

# proto: spec → primitives → assemble → human review

Markup is output tokens (≈5× input price) and, once written, an input cost on every later
turn. So: the expensive model writes the *spec* and the *primitives* once; a cheap model
assembles pages; a human, not the model, reviews the result. Nobody re-reads the HTML.

## Hard rules

- No raw page markup before the spec is approved and the primitives exist.
- Classes, tokens, components only from the team design system. Never invent styles.
- Sample data only from the team sample set, referenced by name; never type rows by hand.
- Shell (`<head>`, CSS/JS links, nav, footer) is written once. Pages are bodies.
- Edits are search-replace on the affected block. Never rewrite a whole file.
- Never read a generated page back into context. Verify with a screenshot, the browser,
  or `bound read page.html --outline` (headings, ids, sections, forms, templates).
- One page per turn. Finish the review loop before starting the next page.

## Phase 0: interview (one message, ≤7 questions)

Skip anything already known. Use the host question tool if available.

1. Hypothesis: what must this prototype prove or let stakeholders decide?
2. Flows and roles: which screens, in what order, for whom?
3. States and rules: statuses, transitions, validations, edge cases that must be visible.
4. Design system: path/URL, how it is loaded, which components exist (name them).
5. Sample data: path/format, which entities are needed.
6. Interaction depth: static, clickable navigation, or working local state (no backend)?
7. Done-check: who reviews, in which browser, what "satisfied" means.

## Phase 1: spec (expensive model, ≤150 lines)

Write `proto/spec.md` (or `.yaml`): pages → sections → components (design-system names),
fields, sample-data refs, states, events/transitions, business rules, open questions.
Ask the human to approve the spec before touching markup. The spec is the artefact the
business actually reviews; markup is a rendering of it.

## Phase 2: primitives (expensive model, once)

Under `proto/primitives/`: the shell, one file per reusable block (card, table, form,
stepper, status badge, modal, empty state…) as a `<template id="…">` or a small JS
render function taking data, plus `app.js` with the state machine from the spec
(states, events, guards). Each primitive ≤40 lines, uses design-system classes only,
takes sample data as an argument. Write once; later changes are search-replace.

Produce `proto/primitives/INDEX.md`: one line per primitive (name, signature, purpose).
This index, not the bodies, is what assembly gets.

## Phase 3: assembly (cheap model)

Delegate page assembly to the cheapest available model (subagent with a fast model, or
switch model). Give it only: the spec section for this page, `INDEX.md`, the shell path,
the sample-data names. Not the primitive bodies, not other pages. Instruction: compose
primitives, wire events to `app.js`, no new styles, no new components, no new sample
rows, output the page file only. If a needed primitive is missing, it reports back
instead of improvising; the expensive model adds the primitive.

## Phase 4: human review loop

Open the page for the human (browser / preview). Do not describe the markup. Ask one
question: "Satisfied with <page>? yes / no".

- yes → mark the page done in the spec, move to the next page or stop.
- no → ask ≤5 questions in one message:
  1. Which section(s) are wrong?
  2. Content, layout, business rule, or flow?
  3. Expected vs actual, in one sentence each.
  4. Is this a spec change (the requirement moved) or a rendering miss?
  5. Anything else blocking sign-off?
  Then: update the spec first if the requirement moved; fix or add a primitive if a
  block is wrong everywhere; otherwise re-assemble only the affected section via the
  cheap model. Repeat the single yes/no question.

## Handoff

If the prototype spans several sessions: `HANDOFF` with spec path, primitive index,
pages done/pending, open questions. No markup in the handoff.
