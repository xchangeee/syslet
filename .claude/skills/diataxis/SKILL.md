---
name: diataxis
description: >-
  Apply the Diátaxis documentation framework when writing, reviewing, structuring,
  or improving technical documentation. Use whenever creating or editing docs —
  tutorials, how-to guides, reference, explanation, READMEs, API docs, guides,
  onboarding material — or when deciding what kind of doc something should be, how
  to organise a documentation set, or diagnosing why a doc "feels wrong". Classifies
  every piece of content by the user need it serves (learning / goals / information /
  understanding) and keeps the four documentation types from bleeding into each other.
---

# Diátaxis

Diátaxis is a systematic way to think about and produce technical documentation. Its
core claim: documentation serves **four distinct user needs**, each answered by a
**different kind of document**, written a **different way**. Most documentation
problems come from mixing these kinds. Your job when applying this skill is to keep
each document serving **exactly one** need.

> Diátaxis serves *the practitioner in a domain of skill*. Every decision below traces
> back to one question: **what does the user need, right now, in relation to this
> craft?**

## The four kinds at a glance

| | Tutorial | How-to guide | Reference | Explanation |
|---|---|---|---|---|
| **Oriented to** | learning | goals | information | understanding |
| **Answers** | "Can you teach me to…?" | "How do I…?" | "What is…?" | "Why…?" |
| **The user is** | at **study** (acquiring skill) | at **work** (applying skill) | at **work** (applying skill) | at **study** (acquiring skill) |
| **Content informs** | action (doing) | action (doing) | cognition (knowing) | cognition (knowing) |
| **Form** | a lesson | a recipe / series of steps | dry, austere description | a discussion |
| **Analogy** | teaching a child to cook | a recipe | nutrition info on the packet | a book on food science/history |

## Step 1 — Always classify first (the compass)

Before writing or fixing anything, decide which kind it is (or should be). Ask **two**
questions:

1. Does the content inform **action** (practical steps, *doing*) or **cognition**
   (theoretical/propositional knowledge, *thinking*)?
2. Does it serve the user's **acquisition** of skill (*study*, learning) or their
   **application** of skill (*work*, getting a job done)?

The two answers give exactly one type:

| If the content informs… | …and serves the user's… | …then it is a… |
|---|---|---|
| action | acquisition of skill | **tutorial** |
| action | application of skill | **how-to guide** |
| cognition | application of skill | **reference** |
| cognition | acquisition of skill | **explanation** |

Use the compass whenever intuition is uncertain — or, worse, gives a confident answer
that a nagging doubt contradicts. Apply it at any zoom level: a whole document, a
section, a single sentence. Use the terms loosely at first (action ≈ doing,
cognition ≈ thinking, acquisition ≈ study, application ≈ work) — don't get stuck on
the exact words.

## Step 2 — One document, one need (the golden rule)

**Never mix the four kinds in a single document.** Blurring the boundaries is the root
of most documentation problems. When a draft or an existing doc mixes needs, split it
and **link between the pieces** rather than cramming.

Two conflations are especially common — resolve them with the *study vs work* test:

- **Tutorial ↔ how-to guide** (the most frequent and most harmful mix). Both are
  practical step-by-step guides, so they look alike. The difference is *only* the need:
  a tutorial gives a **learning experience** to someone at **study**; a how-to guide
  helps a **competent** user complete a task at **work**. This is **not** the same as
  basic vs advanced — a how-to can cover something trivial, a tutorial can teach
  something advanced.
- **Reference ↔ explanation**. Both are theoretical (no steps). Reference is what you
  consult **while working**; explanation is what you read **away from the work**, to
  reflect. Rules of thumb: if it's boring/unmemorable or a list/table → reference; if
  you could imagine reading it in the bath, or it answers "tell me about…" → explanation.

When editing, watch for these leaks and cut or relocate them:

- explanation smuggled into a tutorial → keep one line, link out ("We use HTTPS because
  it's more secure" + link)
- teaching or explanation padding a how-to guide → delete; link to tutorial/explanation
- instruction or "why" creeping into reference → delete; link to how-to/explanation
- steps or reference tables growing inside explanation → move them to their proper home

## Step 3 — Write to the type

Each kind has its own rules and voice. Summary below; read `references/four-types.md`
for the full principles and language patterns, and use `references/templates.md` for a
starting skeleton.

**Tutorial** — *a lesson; take the learner by the hand.*
- The learner learns **by doing** something meaningful toward an achievable goal.
- First rule of teaching: **don't try to teach.** Give things to *do*; trust learning to follow.
- Show where they're going; deliver **visible results early and often**; keep a
  narrative of the expected ("you'll notice…", "the output should look like…").
- Be **concrete and particular**; ensure **reliability** (it must work every time);
  **ruthlessly minimise explanation**; **ignore alternatives/options**; allow repetition.
- Voice: "we", "In this tutorial we will…", "First, do x. Now do y."

**How-to guide** — *directions to a real-world goal, for a competent user.*
- Address a **real user goal/problem**, framed from the **user's** perspective, not the
  machinery's ("To deploy config that matches your needs…", not "press the Deploy button").
- Assume competence; **omit the unnecessary** (usability > completeness); describe a
  **logical sequence**; handle **real-world complexity** (forks, conditionals, "if this, then that").
- Seek **flow**. No teaching, no digressions, no exhaustive reference — link out instead.
- Title exactly what it does: **"How to X"** (a specific verb + goal).

**Reference** — *austere, neutral technical description you consult, don't read.*
- **Describe and only describe.** Accurate, complete, consistent, no opinion or instruction.
- **Adopt standard, consistent patterns**; **mirror the structure of the product/code**
  it documents; provide **examples** for illustration (but never let them drift into explanation).
- Voice: state facts; list commands/options/flags/limits; give warnings where needed.

**Explanation** — *discussion that deepens understanding; answers "why".*
- **Make connections**; **provide context** (design decisions, history, constraints);
  talk **about** the topic (you should be able to prefix each title with an implicit "About…").
- **Admit opinion, perspective, alternatives** — treat it as a discussion.
- **Keep it bounded**: anchor to a real or imagined *why?* question so it doesn't sprawl.

## Step 4 — Structure and naming

- Diátaxis is **an approach, not four boxes**. It *tends* toward four top-level sections
  (Tutorials / How-to guides / Reference / Explanation), each with a landing page — but
  that structure is an *outcome* of good practice, not a template to impose.
- **Never create empty tutorial/how-to/reference/explanation skeletons** and try to fill
  them. Structure emerges **from the inside out** as you improve real content.
- **Landing/contents pages read like overviews** — introductory prose that frames the
  links, not bare lists.
- Keep lists to **~7 items**; break longer ones into named, introduced groups.
- **Reference architecture mirrors the product's architecture** (a method → its class → its module).
- For **complex sets** (multiple user types, platforms, or topics), think **user-first**:
  organise around how *users* see the product. It's fine to nest the four types under
  topics/user-types, or to share some material — as long as forms stay unmuddled. See
  `references/four-types.md` and the structure notes there.

## Workflows

**Writing new documentation**
1. Classify the user need(s) behind the request with the compass (Step 1).
2. If the request bundles needs (e.g. "document feature X"), **split it into separate
   docs** — often one of each type.
3. Draft each doc to its type's rules (Step 3); start from `references/templates.md`.
4. Sweep for boundary leaks (Step 2); relocate or link out anything off-type.
5. Name and place per Step 4.
6. Run the quality gate below.

**Improving / auditing existing documentation** — iterate, don't plan top-down:
1. **Choose something** in front of you — a page, a paragraph, even a sentence.
2. **Assess it**: *What user need does this represent? How well does it serve that need?
   What can be added / moved / removed / changed to serve it better? Do its language and
   logic fit this mode?*
3. **Decide one** next action that produces an immediate improvement.
4. **Do it**, and consider it done — commit/publish it.
5. **Repeat.** Each change reveals the next. Documentation is **never finished but always
   complete** — good at every stage, not waiting on a big-bang rewrite.

## Quality gate (before calling a doc done)

- **Single-need check:** does each document serve exactly one of the four needs? Run the
  boundary tests from Step 2.
- **Functional quality** (Diátaxis *exposes* lapses in these but does not supply them —
  you must): accuracy, completeness, consistency, precision.
- **Deep quality** (what Diátaxis actively fosters): flow, fitting user needs,
  anticipating the user, feeling good to use. It should feel like it *moves with* the reader.

## Reference files (load on demand)

- `references/four-types.md` — full per-type principles, do/don't lists, language
  patterns, the food-and-cooking analogies, and the two boundary deep-dives.
- `references/checklists.md` — authoring checklist and review/audit checklist for each
  type, plus a "is this doc mixing types?" diagnostic.
- `references/templates.md` — copy-paste skeletons for each of the four types and for a
  landing page.

## Pitfalls to avoid

- Treating Diátaxis as a filing scheme instead of a design approach.
- Building empty four-section skeletons.
- Trying to fix structure first — improve content; structure follows.
- Over-explaining in tutorials; describing trivial UI operations in how-to guides;
  editorialising in reference; letting explanation absorb steps and tables.
- Planning a big rewrite instead of shipping small, complete improvements.
