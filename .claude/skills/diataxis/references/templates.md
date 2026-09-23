# Diátaxis templates

Copy-paste skeletons for each of the four types and for a landing page. They are
**starting points**, not forms to fill blindly — delete prompts as you replace them, and
keep each document serving a single need. Written in Markdown; adapt to your toolchain
(reStructuredText, AsciiDoc, HTML, a wiki, etc.).

---

## Tutorial

```markdown
# Getting started with <thing>: build your first <result>

In this tutorial we will <build/create X>. Along the way we will encounter
<the key tools/concepts the learner should become familiar with>.

You don't need to understand everything we do — just follow along, and by the end
you will have <the concrete accomplishment>.

> Before you start, make sure you have <the minimal, guaranteed-to-work prerequisites>.

## Step 1 — <first concrete action with an early visible result>

Do <exactly this>:

    <exact command / action>

You should see something like:

    <exact expected output>

Notice that <the sign the learner should observe>. This means <what it confirms>.

## Step 2 — <next concrete action>

Now, do <exactly this>. After a few moments, <what will happen>.

> If you don't see <expected>, you probably <the likely mistake and the fix>.

## Step 3 — <…continue: one concrete step → one visible result, no options, no detours…>

## What you have done

You have built <the result>, and along the way you used <tools/concepts>. 

Where to go next:
- To accomplish specific tasks with <thing>, see the how-to guides.
- To understand *why* <thing> works this way, see <explanation link>.
```

Reminders: single successful path, no alternatives; results visible at every step;
explanation minimised and linked; concrete and particular; must work every time.

---

## How-to guide

```markdown
# How to <achieve a specific real-world goal>

This guide shows you how to <goal>. It assumes you are already familiar with
<assumed competence / prerequisites>.

## Before you begin

- <preconditions / what must be true or in place>

## Steps

1. To <sub-goal>, do <action>.
2. <Next action in logical sequence.>
3. If you want <variant A>, do <x>. To handle <variant B> instead, do <y>.
   <— fork/branch for real-world cases where relevant.>

## Verify

Confirm success by <observable check>.

## Related

- Full options: see the <reference> guide.
- Background on why this works: see <explanation>.
```

Reminders: frame around the user's goal, not the machinery; assume competence; omit the
unnecessary; allow forks; link out instead of teaching/explaining; title is a precise
"How to…".

---

## Reference

```markdown
# <Name of the component / command / API> reference

<One-line neutral statement of what this is.>

## Synopsis / signature

    <exact signature, syntax, or invocation>

## Description

<Neutral, factual description of behaviour. No instructions, no rationale.>

## Parameters / options / fields

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `<name>` | `<type>` | `<default>` | <neutral description> |

## Returns / output

<What it returns/produces, stated as fact.>

## Errors / limits / constraints

- `<error>` — <when it occurs>.
- You must not <constraint>.

## Example

    <minimal illustrative example — no explanatory narrative>
```

Reminders: describe and only describe; consistent standard patterns; structure mirrors
the product; examples illustrate but don't explain; austere and authoritative.

---

## Explanation

```markdown
# About <topic>

<Frame the topic and the "why?" question this discusses.>

## Background / context

<Why things are the way they are: design decisions, history, constraints.>

## How <topic> fits with <the bigger picture>

<Make connections to related concepts, inside and outside the immediate topic.>

## Alternatives and trade-offs

<Weigh different approaches; admit opinion and perspective.>
Some teams prefer <X> because <reason>; this works well when <condition>, but <trade-off>.

## Further reading

- <links to related explanation, reference, or external material>
```

Reminders: discuss *about* the topic; make connections; provide context and reasons;
weigh alternatives and opinions; keep it bounded to a topic/why-question; no steps, no
big tables.

---

## Landing / contents page

A landing page (home page, or the top of each section) should **read like an overview** —
introductory prose that frames the links, not a bare list. Keep groups to ~7 items.

```markdown
# How-to guides

<One or two sentences describing what this section helps the reader do.>

## Installation

<Short intro sentence for this group.>

- [Local installation](...)
- [Docker](...)
- [Virtual machines](...)

## Deployment and scaling

<Short intro sentence for this group.>

- [Deploy an instance](...)
- [Scale your application](...)
```

Typical overall shape (an *outcome* of good practice, not a mandate — never create empty
sections to fill later):

```text
Home                      <- landing page (overview)
    Tutorials             <- landing page
        <part 1, part 2, …>
    How-to guides         <- landing page
        <install, deploy, scale, …>
    Reference             <- landing page (mirrors the product's structure)
        <CLI, endpoints, API, …>
    Explanation           <- landing page
        <architecture, security, performance, …>
```
