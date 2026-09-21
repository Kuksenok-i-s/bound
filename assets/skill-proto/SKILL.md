---
name: proto
description: Token-frugal HTML prototyping for pre-production research (hypothesis pages, business-logic mockups, clickable flows). Use whenever the user asks for an HTML page, mockup, prototype, dashboard, flow or screen using the team's own design system and sample data. Enforces spec → reusable primitives → assembly by a cheaper model → human review loop. Do not use for production frontend components.
---

# proto

Markup is output tokens (≈5× input) and then sits in context every turn. Write the
spec once, write markup once, never read it back, let a human judge it.

## Hard rules

- Design-system classes/components only; sample data by reference, never typed rows.
- Shell (`<head>`, styles, nav, footer) once. Pages are bodies.
- Edits are search-replace on the affected block. Never rewrite a file.
- Never read a generated page back. Verify with a screenshot or
  `bound read page.html --outline` (headings, landmarks, forms, templates, ids).
- Data for the page comes from one script/command run once; embed its output.

## 0. Interview (one message, ≤7)

Hypothesis to prove · flows and roles · states/rules that must be visible · design
system location and component names · sample data location · interaction depth
(static / clickable / local state) · who reviews and what "satisfied" means.
Skip anything already known.

## 1. Spec (≤100 lines) — human approves before markup

Pages → sections → components (design-system names), fields, data refs, states,
events, rules, open questions. The spec is what the business reviews.

## 2. Choose the path

- **Direct**: one page, no design system, no iterations expected → write the page in
  one Write. Stop here; go to review.
- **Primitives**: ≥2 pages, a design system, or a review loop expected → one file per
  reusable block (≤40 lines, takes data), `app.js` state machine, `INDEX.md` (name,
  signature, purpose). Assemble pages from primitives, preferably via the cheapest
  available model given only the page's spec section + `INDEX.md` + shell path. If a
  primitive is missing it reports back; do not improvise styles or components.
  Re-assembly after a change costs no model tokens if a build script does it.

## 3. Human review

Open the page. Ask one question: "Satisfied with <page>? yes / no".
yes → mark done, next page or stop.
no → ≤5 questions in one message: which section · content/layout/rule/flow ·
expected vs actual · requirement moved or render miss · anything else blocking.
Then: spec first if the requirement moved; primitive if wrong everywhere; else fix the
one section. Repeat the yes/no.

Handoff across sessions: spec path, primitive index, pages done/pending, open questions.
No markup.
