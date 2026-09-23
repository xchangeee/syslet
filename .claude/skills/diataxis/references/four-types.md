# The four types in depth

Full guidance for each of the four documentation types, followed by the two boundary
deep-dives (tutorial vs how-to, reference vs explanation) and notes on structure. This
distils the Diátaxis framework by Daniele Procida (https://diataxis.fr,
CC-BY-SA 4.0).

The two axes underneath everything:

- **action ↔ cognition** — knowing *how* (what we do) vs knowing *that* (what we think).
- **acquisition ↔ application** — being **at study** (acquiring a skill) vs being **at
  work** (applying it).

These two dimensions completely define the territory of a craft, which is why there are
**exactly four** kinds of documentation — no more, no fewer.

---

## Tutorials — learning-oriented

> A tutorial is an **experience** that takes place under the guidance of a tutor. It is
> always **learning-oriented**. Analogy: teaching a child to cook — what matters is what
> the child *gains*, not the dish they produce.

A tutorial is a **lesson**: a practical activity in which the student learns *by doing*
something meaningful toward an achievable goal. It serves **study** (acquisition of
skill), not the completion of real work. What the learner *does* is not necessarily what
they *learn* — through doing, they pick up concepts, names, tools, workflows, confidence.

The hard truth of tutorials: the instructor is **required to be present but condemned to
be absent**. You must build the whole learning experience into written instruction, with
no chance to correct mistakes in the moment.

The teacher owns nearly all the responsibility. The exercise you set must be:

- **meaningful** — the learner feels a sense of achievement
- **successful** — the learner can actually complete it
- **logical** — the path makes sense
- **usefully complete** — it touches all the actions, concepts and tools the learner
  needs to become familiar with

### Key principles

- **Don't try to teach.** Provide an experience through which the learner *can* learn.
  Give them things to *do*; only the learner can do the learning.
- **Show the learner where they'll be going.** Set expectations up front ("In this
  tutorial we will build and deploy a scalable web app…"). *Not* "you will learn…"
  (presumptuous).
- **Deliver visible results early and often.** Every step should produce a comprehensible
  result, however small, so cause and effect connect.
- **Maintain a narrative of the expected.** Continuously reassure: "You'll notice…",
  "After a few moments the server responds with…". Show expected output. Flag likely
  wrong turns ("If you don't see…, you probably forgot to…").
- **Point out what the learner should notice.** Close the loops of learning by drawing
  attention to signs they'd otherwise miss (e.g. how the prompt changes).
- **Target the feeling of doing.** Tie purpose to action so a confident rhythm develops.
- **Encourage and permit repetition.** Learners repeat steps to confirm the result;
  design for that.
- **Ruthlessly minimise explanation.** The user is focused on doing; explanation blocks
  learning. One line max, then link out. This is the hardest temptation to resist.
- **Focus on the concrete and particular** — *this* action, *this* result. General
  understanding emerges from concrete experience, not the reverse.
- **Ignore options and alternatives.** Guide to one successful conclusion; leave
  diversions for later. Keeps the tutorial short and the cognitive load low.
- **Aspire to perfect reliability.** Confidence is built layer by layer and easily shaken.
  It must work for every user, every time — only extensive testing/observation reveals
  the flaws you can't see yourself.

### Anti-pedagogical temptations (resist these)

abstraction/generalisation · explanation · choices · information

### Language of tutorials

- "We…" — the first-person plural: *we're in this together*.
- "In this tutorial, we will…" — state what the learner will accomplish.
- "First, do x. Now, do y. Now that you've done y, do z." — no ambiguity.
- "We must do x before y because … (see [explanation])." — minimal explanation, linked.
- "The output should look something like…" — clear expectations.
- "Notice that… Remember that… Let's check…" — orientation and reassurance.
- "You have built a …" — describe (and mildly admire) what they accomplished.

---

## How-to guides — goal-oriented

> How-to guides are **directions** that guide the reader through a problem toward a
> result. They are **goal-oriented**. Analogy: a recipe.

A how-to guide helps an **already-competent** user get something done. It serves **work**
(application of skill), navigating a real-world problem-field. Examples: *How to
calibrate the radar array*, *How to configure reconnection back-off policies*. Not a
how-to: *How to build a web application* — too open-ended, not a specific goal.

A rich list of how-to guides advertises what your product can *do*, and these are often
the most-read pages in a documentation set.

### Key principles

- **Address real problems, from the user's perspective — not the machinery's.** A how-to
  answers to a *human project*. Don't narrate the tool's motions ("press the Deploy
  button"); address the need ("to deploy a configuration that matches your workload…").
  Tools are incidental bit-players; the user's goal defines the guide, and a guide often
  cuts across several tools.
- **Don't state the obvious.** Anyone competent in the domain knows how a power switch or
  a standard interface works. Give them what they *can't* trivially infer.
- **Omit the unnecessary.** Practical usability beats completeness. Unlike a tutorial, a
  how-to need not be end-to-end — start and end somewhere reasonable and let the user
  join it to their own work.
- **Provide an executable solution** in the form of a contract: *in this situation, these
  steps get you through.* "Actions" include thinking and judgement, not just physical acts.
- **Describe a logical sequence.** Order by necessity, or by what best sets up the user's
  thinking even when steps could technically be reordered.
- **Address real-world complexity.** Real problems fork and branch, with multiple entry
  and exit points; users often must apply judgement. Make the guide adaptable rather than
  useful for one narrow case only.
- **Seek flow.** Ground sequences in how the user actually thinks and acts. Avoid
  needless context-switching; mind pace and rhythm. At its best a how-to *anticipates*
  the user — like a helper putting the next tool in your hand.
- **Pay attention to naming.** Title says exactly what the guide shows:
  - good: *How to integrate application performance monitoring*
  - bad: *Integrating application performance monitoring* (maybe it's about *whether* to)
  - very bad: *Application performance monitoring* (what? whether? why? how?)

### What a how-to guide is **not**

- Not a tutorial (see boundary below) — the single most damaging conflation.
- Not merely a linear procedure — solving a real problem may require branches and judgement.

### Language of how-to guides

- "This guide shows you how to…" — state the problem/task clearly.
- "If you want x, do y. To achieve w, do z." — conditional imperatives.
- "Refer to the x reference guide for a full list of options." — link, don't inline
  exhaustive reference.

---

## Reference — information-oriented

> Reference guides are **technical descriptions** of the machinery and how to operate it.
> Reference is **information-oriented**. Analogy: the nutrition/ingredients information on
> a food packet.

Reference contains **propositional/theoretical knowledge** the user consults **while
working**. Unlike tutorials and how-tos (led by user needs), reference is **led by the
product it describes**. Users need it for **truth and certainty** — a firm platform to
stand on while they work. One doesn't *read* reference; one **consults** it.

### Key principles

- **Describe and only describe.** Neutral description is the whole job — accurate,
  precise, complete, clear. Resist the natural urge to instruct, explain, or opine; if
  those are needed, **link** to how-to guides, explanation, or tutorials.
- **Be austere and authoritative.** No doubt, no ambiguity, no distraction. It's like a
  map: it tells you about the territory so you don't have to go check yourself.
- **Adopt standard patterns.** Reference is useful because it's **consistent** — put
  material where users expect it, in a familiar format. This is not the place for stylistic
  flair.
- **Respect the structure of the machinery.** The documentation's structure should
  **mirror the product's** (method → class → module), so users navigate both together.
  Don't force an unnatural structure; let the code's logical arrangement guide it. (This
  also makes gaps in coverage visible.)
- **Provide examples.** Examples illustrate succinctly without tipping into explanation —
  but keep them from drifting into *why* or *what-if* (that's explanation's job).
- Auto-generation (e.g. API docs from source) is a powerful way to keep reference
  faithful — but auto-generated reference is *not* all the documentation a product needs.

### Style and form

austere and uncompromising · neutral, objective, factual · structured according to the
structure of the machinery itself.

### Language of reference

- "`django.utils.log.DEFAULT_LOGGING` is defined in `django/utils/log.py`." — state facts
  about the machinery and its behaviour.
- "Sub-commands are: a, b, c, d." — list commands, options, flags, limits, errors.
- "You must use a. You must not apply b unless c. Never d." — warnings where appropriate.

---

## Explanation — understanding-oriented

> Explanation is a discursive treatment of a subject that permits **reflection**. It is
> **understanding-oriented**. Analogy: a book on the history/science of food — read to
> reflect, not while cooking.

Explanation deepens and broadens understanding; it brings clarity, context, and joins
things together. It answers **"why?"** and **"can you tell me about…?"**. Its viewpoint
is **higher and wider** than the others — it takes a *topic* as its scope. It's the one
kind of documentation you might read away from the product (even "in the bath").

Understanding doesn't *come from* explanation, but explanation weaves the web that holds
a practitioner's knowledge together. Without it, knowledge is fragmented, fragile, and
the practitioner works *anxiously*. It's less **urgent** than the other three but no less
**important**.

Explanation is often not consciously recognised — it ends up scattered in small parcels
inside other docs. Give it its own place.

### Key principles

- **Make connections** — to other parts of the subject and beyond it, to build a web of
  understanding.
- **Provide context** — explain *why*: design decisions, historical reasons, technical
  constraints; draw implications; give examples.
- **Talk *about* the subject.** Explanation is *around* a topic. You should be able to put
  an implicit (or explicit) "About…" before each title: *About user authentication*.
- **Admit opinion and perspective.** Real understanding includes judgement and
  alternatives. Weigh counter-examples and different approaches — treat it as a discussion.
- **Keep it closely bounded.** Explanation tends to absorb instruction and description;
  don't let it. Anchor it to a real or imagined **why?** question, or draw reasonable
  lines around a topic and stop there. (Its open-endedness is its main writing hazard —
  it's not always clear where to start or stop.)

### Things to discuss

the bigger picture · history · choices, alternatives, possibilities · reasons and
justifications. It can also be called *Discussion*, *Background*, *Conceptual guides*, or
*Topics* — it need not be labelled "Explanation".

### Language of explanation

- "The reason for x is that historically, y…" — explain.
- "W is better than z, because…" — offer judgements/opinions where appropriate.
- "An x in system y is analogous to a w in system z. However…" — provide context.
- "Some users prefer w (because z). This can be a good approach, but…" — weigh alternatives.
- "An x interacts with a y as follows…" — unfold internal workings to explain *why*.

---

## Boundary deep-dive 1: tutorial vs how-to guide

The most common and most harmful conflation. They look alike — both are practical,
step-by-step, promise success if followed, and only make sense to someone with hands on
the machinery. The difference is **the need served**: **study** vs **work**.

| Tutorial (study) | How-to guide (work) |
|---|---|
| Helps the pupil **acquire basic competence** | Helps a **competent** user perform a task correctly |
| Provides a **learning experience** (what the learner *does* and experiences) | **Directs the user's work** toward a result |
| **Carefully managed path** with required encounters | Aims for a result; **the path can't be managed** (real world) |
| **Familiarises** the learner with tools/language/processes | **Assumes familiarity** with them |
| **Contrived, safe** learning setting; the unexpected is eliminated | The **real world**; must **prepare for the unexpected** |
| **Single line**, no choices | **Forks and branches**: "if this, then that" |
| **Must be safe** — always possible to start over | **Cannot promise safety** — often one chance to get it right |
| **Responsibility lies with the teacher** | **The user is responsible** for getting in/out of trouble |
| **Concrete and particular** (known tools/materials) | **General** (specifics unknowable in advance) |
| Explicit about basic embodied things (where to type, how long to wait) | Relies on that as **implicit knowledge** |

**Not basic vs advanced.** A how-to can cover mundane basics (filling in paperwork); a
tutorial can teach something highly advanced ("Difficult neonatal intubations" for
experienced anaesthetists). The distinction is always **study vs work**.

Why it matters: a document that tries to teach *and* guide real work at once serves
neither. In safety-critical fields it's literally deadly; in software it quietly drives
newcomers away.

---

## Boundary deep-dive 2: reference vs explanation

Both belong to the **theory** (cognition) half — no steps. The difference is again
**application vs acquisition** (work vs study).

- **Reference** is what you turn to **while working**, to *apply* knowledge — a tidal
  chart, tables of figures.
- **Explanation** is what you turn to **away from work**, to *acquire* understanding — an
  article on *why* there are tides.

Rules of thumb:

- **Boring/unmemorable → reference.**
- **Lists and tables** (classes, methods, attributes) → reference.
- **Could you read it in the bath? Does it answer "tell me more about…"? → explanation.**

The slip usually happens when reference "becomes expansive" — an illustrative example
grows into a *why/what-if* story. That harms the reference (interrupted by digression)
*and* the explanation (never allowed to develop properly). Keep them separate.

The real test when in doubt: **would the reader consult this while executing a task, or
after stepping away to think?** (When intuition fails, fall back to the compass.)

---

## Notes on structure and complex sets

- The typical shape: `Home` → `Tutorials` / `How-to guides` / `Reference` / `Explanation`,
  each a section with a **landing page** that overviews its contents. Add another layer of
  hierarchy within a section when groups get large (e.g. Install → Local / Docker / VM).
- **Contents/landing pages** should *introduce*, not just list — headings plus short
  intro prose that give context. You're authoring for a human, not satisfying a scheme.
- **Lists longer than ~7 items** are hard to read unless mechanically ordered; break them
  into smaller, named groups.
- **Two-dimensional problems** — when the four types meet another structure (multiple user
  types like users/developers/contributors; multiple platforms; distinct topic areas):
  - Diátaxis is **an approach, not four boxes**. There need not be exactly four top-level
    divisions.
  - Think **user-first**: document the product *as it is for the user*. If "product on
    land / sea / air" is effectively three products for three users, start there.
  - It's fine to nest the four types under topics/user-types, share some material, or let
    one audience's content follow another's — **as long as the four forms stay unmuddled**.
  - **Let documentation be as complex as it needs to be.** Complex structures are fine if
    they're logical and fit user needs.
