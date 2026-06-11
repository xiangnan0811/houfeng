---
date: 2026-04-22
status: historical-stub
---

# Historical V1 design bundle

This directory used to contain the first large design bundle for Houfeng. The old text mixed useful product thinking with obsolete phase boundaries, outdated object names, and rigid requirements that no longer serve the project.

The maintained design guidance now lives in [`../current/`](../current/README.md). Use that directory, current code, task artifacts, and `.trellis/spec/` for planning and implementation.

## What remains useful

- Houfeng is a quiet, single-operator control plane rather than a generic enterprise platform.
- Runtime observation, service entrypoint probing, and asset evidence should stay legible and honest.
- Dangerous lifecycle actions need clear review, confirmation, and audit trails.
- Agents should remain bounded and should not become arbitrary script executors.

## Historical traceability

The full former V1 text was removed from tracked docs to avoid reintroducing obsolete boundaries. Use git history if you need to inspect the old bundle.
