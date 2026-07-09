# Technical Design

## Scope

This task reviews and verifies the current frontend branch changes in-place. It does not introduce a new UI direction, backend API contract, browser automation dependency, or release workflow.

## Boundaries

- Source of truth for frontend conventions: `.trellis/spec/web/*`, `docs/design/current/*`, and current code.
- Styling remains pure CSS with design tokens and BEM-like class names.
- Browser sanity remains local-only evidence through an already available Chromium/CDP path; no Playwright/Cypress/WebDriverIO dependency is added.
- Trellis artifacts document and gate this work, but commits are not made unless separately requested.

## Review Strategy

1. Inspect staged and unstaged git state to avoid confusing prior branch work with new fixes.
2. Review high-risk behavior diffs:
   - subscription settings save flow and partial-update prevention;
   - login layout CSS and bundled/global CSS alignment;
   - CSS pseudo-element contracts;
   - style imports, package updates, Vite config, and generated artifacts.
3. Use automated tests for behavior and CSS contract regressions.
4. Use local browser geometry checks for visible login layout, because jsdom cannot prove layout.
5. Run `make verify-web` as the final quality gate.

## Data Flow / Contracts

- Settings saves should not persist subscription changes if the pending budget edit is invalid.
- UI save controls should not allow repeated saves while a save request is in flight.
- Login page footer should be a vertical sibling below the card, not a horizontal flex item beside it.
- `watchtower-header` eyebrow pseudo elements should only render where explicit variants opt in.
- `.section-title::before` should position against `.section-title` itself.

## Rollback Notes

All fixes are staged but uncommitted. If a new review finding is invalid, revert only that local hunk rather than resetting the branch. If final verification fails, stop and report the failing command and root cause.
