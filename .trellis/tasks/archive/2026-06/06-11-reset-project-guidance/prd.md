# Reset project guidance documents

## Goal

Remove stale V1/V2-era authority and rigid version-bound constraints from the maintained project guidance so future design, exploration, planning, and development are not forced through outdated trial-and-error conclusions. Keep the useful positive guidance that still reflects the current repository: honest evidence levels, safety boundaries, real deployment limits, current implementation patterns, and reusable design intent.

This task is a documentation and agent-guidance reset, not a product feature. It should make the docs easier to evolve while keeping the project honest about current code and operational risk.

## Confirmed Facts

- Houfeng is early-stage and still changing. The README already says it is not production-ready packaging and that no completed real-inventory validation should be claimed.
- Maintained public docs still expose version labels as if they are active organizing principles:
  - before this task, `README.md` pointed to `docs/operations/v1-smoke-run.md`, `docs/operations/v2-visual-evidence.md`, `docs/design/v1-baseline/`, and `docs/design/v2-houfeng/`.
  - `docs/README.md` labels current operator/design references around `v1` and `v2`.
- Trellis agent specs currently point backend and web work at a frozen `docs/design/v1-baseline/` subset for business/structure and `docs/design/v2-houfeng/` for visuals.
- `docs/design/v1-baseline/README.md` says the structural sections remain "frozen and authoritative" and includes "v1 to this point" language that can block new exploration.
- `docs/design/v2-houfeng/design-language.md` and `component-spec.md` include useful visual intent and component guidance, but also use versioned authority and hard-contract language that reads as permanent.
- `docs/operations/v2-visual-evidence.md` contains useful current browser-sanity workflow guidance, but its title and references frame it as a V2 workflow.
- `docs/operations/v1-smoke-run.md` is operationally useful, but the filename and references frame it as V1. Version strings like `v1.2.3` in release examples are semver examples and should not be treated as design-version doctrine.
- Archived Trellis tasks contain historical V1/V2 references. Those are task history and should not be rewritten unless they are actively linked as current authority.

## Requirements

- Current maintained docs must stop presenting V1/V2 baselines as the default authority for future design or implementation.
- Replace versioned authority with non-versioned, current guidance:
  - historical/reference material may remain for traceability;
  - current guidance should be described as living, revisable, and evidence-based;
  - hard constraints should remain only when they protect correctness, security, data integrity, privacy, or verified operational safety.
- Public docs should guide readers through current concepts and workflows without implying that early design bundles freeze future direction.
- Trellis specs should direct agents to current code, current specs, and living docs first; historical design files may be cited only as background or source context, not as a reason to block new product direction.
- Operational docs should keep useful commands, evidence levels, privacy cautions, and deployment caveats while removing V1/V2 labels from maintained workflow names where practical.
- Do not rewrite archived Trellis task history or git-history-only design material.
- Do not remove real safety guidance such as:
  - no false production-readiness claims;
  - token/credential/real-data secrecy;
  - migration and transaction discipline;
  - no unverified backend/provider/billing truth claims;
  - current installer/release asset requirements;
  - honest UI evidence levels and local screenshot policy.

## Acceptance Criteria

- [ ] Maintained public indexes (`README.md`, `docs/README.md`) point to non-versioned current guidance names or explicitly mark old versioned design directories as historical/reference only.
- [ ] Maintained operation workflow references no longer use V1/V2 labels as active workflow authority, except semver examples such as `VERSION=v1.2.3`.
- [ ] Trellis backend/web spec authority headers no longer say `docs/design/v1-baseline/` is frozen authoritative or `docs/design/v2-houfeng/` is the visual authority.
- [ ] Agent-facing web/backend specs still preserve current actionable engineering and safety rules.
- [ ] Current design guidance preserves useful visual/product intent but removes or softens stale version framing and hard-contract wording that blocks exploration without protecting correctness.
- [ ] Historical design files are clearly labeled as historical/reference and non-authoritative for future direction.
- [ ] Repository search confirms no maintained, non-archived docs still describe V1/V2 as active/frozen authority.
- [ ] Link/path changes are internally consistent, with no references to deleted or renamed maintained docs.
- [ ] Documentation-only verification is run with focused search/link checks and, where practical, the repository doc-related quality gate.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
