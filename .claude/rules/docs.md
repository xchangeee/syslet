---
paths:
  - "docs/**"
---

# Documentation

When writing docs markdown files, do not hard-wrap, put each sentence on a new line, separate paragraphs with two newlines.

## Structure

The site is built with zensical: `make docs` builds it with `--strict`, which fails on broken nav entries and links, and `make docs-serve` serves it with live reload.

Pages are organized by [Diataxis](https://diataxis.fr/) quadrant (`tutorials/`, `how-to/`, `explanation/`, `reference/`).
Navigation is defined explicitly in `zensical.toml`'s `nav` table, so a new page must be added there, and to its quadrant's `index.md`.

## How-to headings

How-to headings aren't numbered.
A procedure's steps are the page's H2 sections, in order.
Tasks the reader does instead of or after the procedure, such as removing what it added, go under a final `## Related tasks` as H3 sections, so they can't be mistaken for the next step.
Caveats that belong to a step go into that step, not into a section of their own.

## Tutorial code files

Files the tutorials ask the reader to create live in `docs/tutorials/snippets/<tutorial>/`, as they look at the end of that tutorial; a version from an earlier step goes into `step-N/`.
Include them with the snippets extension instead of writing them inline, with the file name as title:

````md
```cue title="web01.cue"
--8<-- "05-protect-a-container/web01.cue"
```
````

When a step adds or changes lines in an existing file, show the complete new file and highlight those lines with `hl_lines`, e.g. `hl_lines="3 20-23"`, instead of showing the lines separately.
Commands and command output stay inline.
