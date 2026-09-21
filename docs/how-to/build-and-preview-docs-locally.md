# Build and preview the docs locally

This site is built with [zensical](https://zensical.org/), invoked via [uv](https://docs.astral.sh/uv/)'s `uvx` (so `uv` must be installed locally; zensical itself is not a project dependency).

## Build once

```sh
make docs
```

Runs `uvx zensical build --strict`. `--strict` fails the build on any broken `nav` entry or link, so a missing page shows up immediately rather than silently disappearing from the site.

## Serve with live reload

```sh
make docs-serve
```

Runs `uvx zensical serve`, which serves the site locally and rebuilds on file changes. Use this while editing docs.

## Structure

Pages live under `docs/`, organized by [Diataxis](https://diataxis.fr/) quadrant (`tutorials/`, `how-to/`, `explanation/`, `reference/`); navigation is defined explicitly in `zensical.toml`'s `nav` table, not inferred from the directory tree, so a new page must also be added there to appear in the site.

`make clean` removes the built site (`site/`) along with other build artifacts.
