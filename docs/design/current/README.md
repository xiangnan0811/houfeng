# Current design guidance

This directory is the maintained design entry point for Houfeng. It replaces the old habit of treating early `v1` and `v2` design bundles as fixed authority.

Houfeng is still early-stage. These documents record the current product shape, implementation defaults, and safety boundaries that have survived real development. They are living guidance: a task may change them when current code, user intent, or new evidence shows a better direction.

## How to use this directory

- Start with `product-and-architecture.md` for the current product model, deployment topology, and durable safety boundaries.
- Use `interface-language.md` for UI tone, density, state language, evidence levels, and visual defaults.
- Use `component-patterns.md` for current component and page composition patterns.
- Use historical folders such as `../v1-baseline/` and `../v2-houfeng/` only as stubs that point to git history. They do not freeze future product direction.

## Change rule

Hard requirements in current docs should protect one of these things:

- security and privacy;
- database or data-integrity correctness;
- truthful evidence and validation claims;
- dangerous lifecycle actions;
- current code/API contracts that callers depend on.

Everything else is a strong default, not a permanent boundary. If a future task needs a different product flow, visual style, component pattern, or page structure, update the current docs with the decision and evidence instead of forcing the task through historical version labels.

## Historical references

- `../v1-baseline/` records where the early architecture, data-model, rules, interaction, and technology-selection bundle used to live.
- `../v2-houfeng/` records where the previous visual-language and component-spec bundle used to live.

Useful parts have been carried into this current guidance. The old full text is available through git history if needed.
