# Diátaxis checklists

Use these when authoring a new document, reviewing an existing one, or auditing a whole
set. Each type has an **authoring** checklist (write to this) and a **review** checklist
(catch these lapses). Start with the cross-cutting diagnostic.

---

## Diagnostic: is this document mixing types?

Run this first on any doc that "feels wrong". Answering "yes" to any question means a
boundary is being crossed — split the content and link between the pieces.

- [ ] Does one document try to serve **more than one** of: learning, a goal, information,
      understanding?
- [ ] Does a **tutorial** stop to explain *why* at length, offer options/alternatives, or
      assume the reader already knows things?
- [ ] Does a **how-to guide** teach, explain background, narrate trivial UI operations, or
      try to be exhaustive like reference?
- [ ] Does **reference** instruct, argue *why*, give opinions, or wander from neutral
      description?
- [ ] Does **explanation** contain step-by-step procedures or big reference tables that
      belong elsewhere?
- [ ] If you apply the **compass** (action/cognition? acquisition/application?) to the
      opening and to the middle, do you get **different answers**? (A sign of drift.)

---

## Tutorial

**Authoring**
- [ ] It's a **lesson**: the learner learns *by doing* something meaningful.
- [ ] States up front what the learner will accomplish (not "you will learn…").
- [ ] Every step yields a **visible, comprehensible result**.
- [ ] Keeps a **narrative of the expected** (what they'll see; likely wrong turns flagged).
- [ ] **Concrete and particular** throughout; no abstraction/generalisation.
- [ ] Explanation **ruthlessly minimised** — one line max, then a link.
- [ ] **No options or alternatives** — a single successful path.
- [ ] Uses "we"; addresses the learner directly and warmly.
- [ ] **Reliable**: works for every reader, every time (tested end-to-end).
- [ ] Points out what to **notice**; permits/encourages **repetition**.

**Review — cut or fix if present**
- [ ] Paragraphs of explanation → reduce to one line + link.
- [ ] "You could also…" digressions → remove.
- [ ] Assumes prior competence → make basics explicit.
- [ ] Steps whose result the reader can't see/verify → add expected output.
- [ ] "You will learn…" framing → replace with "we will build/create…".

---

## How-to guide

**Authoring**
- [ ] Addresses a **real user goal/problem**, framed from the **user's** perspective.
- [ ] Assumes a **competent** user; skips the obvious.
- [ ] A clear, **logical sequence** of actions (incl. thinking/judgement).
- [ ] Handles **real-world complexity** — forks, conditionals, "if this, then that".
- [ ] **Omits the unnecessary**; usable rather than exhaustive.
- [ ] Has **flow**; minimises context-switching; good pace/rhythm.
- [ ] Title is **"How to X"** — states exactly what it shows.
- [ ] Links out to reference/explanation instead of inlining them.

**Review — cut or fix if present**
- [ ] Teaching/hand-holding → belongs in a tutorial; remove or link.
- [ ] Background/"why" → move to explanation; link.
- [ ] Exhaustive option dumps → move to reference; link.
- [ ] "Turn on the device using the power switch"–level trivia → delete.
- [ ] Machinery-first framing ("press Deploy") → reframe around the user's goal.
- [ ] Vague/ambiguous title → rewrite as a specific "How to…".

---

## Reference

**Authoring**
- [ ] **Describes and only describes** — neutral, factual, complete, precise.
- [ ] **Consistent standard patterns**; material where users expect it.
- [ ] Structure **mirrors the product/code** it documents.
- [ ] Includes **examples** for illustration (that don't drift into explanation).
- [ ] Warnings/constraints stated plainly where they matter.
- [ ] Austere: built to be **consulted**, not read cover to cover.

**Review — cut or fix if present**
- [ ] Instructional "how to" content → move to a how-to guide; link.
- [ ] "Why"/rationale/opinion → move to explanation; link.
- [ ] Examples grown into narratives → trim back to illustration.
- [ ] Inconsistent structure/naming vs the rest of the reference → normalise.
- [ ] Structure that doesn't match the product's architecture → realign (exposes gaps).

---

## Explanation

**Authoring**
- [ ] Anchored to a **why?** question or a clearly bounded topic.
- [ ] **Makes connections** and **provides context** (design, history, constraints).
- [ ] Talks **about** the subject — title works with an implicit "About…".
- [ ] **Weighs alternatives, perspectives, opinions** where useful.
- [ ] **Bounded** — doesn't absorb steps or reference material.

**Review — cut or fix if present**
- [ ] Step-by-step procedures → move to tutorial/how-to; link.
- [ ] Big reference tables/lists → move to reference; link.
- [ ] Sprawl with no clear scope → bound it to a topic/why-question.
- [ ] Title that reads like a task or a fact → reframe as a topic ("About…").

---

## Whole-set audit (iterative, not big-bang)

- [ ] Pick **one** small thing (page, section, sentence) — don't hunt for the worst.
- [ ] Ask: *What user need is this? How well does it serve it? What single change adds/
      moves/removes/changes to serve it better? Do language and logic fit the mode?*
- [ ] Make **one** improvement and ship it (commit/publish).
- [ ] Confirm the doc is **complete** at its current stage (useful now), even if not finished.
- [ ] Repeat. Let structure emerge from the improved content — don't impose it up front,
      and never create empty type-sections to fill later.

## Final quality gate

- [ ] **Single need** per document (diagnostic above passes).
- [ ] **Functional quality**: accurate, complete, consistent, precise. *(Diátaxis exposes
      lapses here but doesn't supply them — verify against the real product.)*
- [ ] **Deep quality**: has flow, fits the user's need, anticipates the user, feels good
      to use.
