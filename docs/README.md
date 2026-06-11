# Houfeng documentation index

This index keeps the current public/operator path separate from design references and local validation workflows. Completed roadmap, release-gate, historical archive, and one-off process/evidence logs have been removed from tracked docs rather than kept as in-repository archive copies.

## Start here

- `../README.md` — public project overview, quick start, components, verification commands, and documentation map.
- `deploy/local-and-systemd.md` — canonical deployment guide for local, Docker Compose center, systemd center installs, and one-command Linux agent onboarding.
- `operations/fresh-install-smoke-run.md` — fresh-install smoke run. The primary onboarding path is the center-generated one-command installer.
- `design/current/README.md` — maintained design guidance and change rule.

## Current operator guides

- `deploy/local-and-systemd.md` — build artifacts, center environment, Docker Compose center deployment (`houfeng` service on `127.0.0.1:16001`, `./data/postgres/`, `./data/logs/`, Release Please -> GitHub Release -> Docker image publishing), authentication, reverse proxy/TLS notes, systemd examples, generated install commands, checksum verification, and manual troubleshooting fallback.
- `deploy/systemd/houfeng-center.service` — example center systemd unit.
- `deploy/systemd/houfeng-agent.service` — example agent systemd unit for manual installs and reference.
- `operations/fresh-install-smoke-run.md` — live PostgreSQL smoke path for center/auth/monitoring instance/agent/target/probe/incident/event checks.
- `operations/ui-preview-and-browser-sanity.md` — frontend preview, browser sanity, local screenshot policy, and protected-route mock/local-center data-source rules.
- `operations/asset-ledger-real-data-validation-readiness.md` — non-sensitive sample, import dry-run/import workflow, authenticated browser sanity, and real-data privacy checklist.
- `operations/asset-ledger-local-sample.json` — fake local sample for Asset Ledger dry-run/import validation.

## Current design guidance

- `design/current/README.md` — entry point, historical-reference boundary, and change rule.
- `design/current/product-and-architecture.md` — current product shape, topology, model, and durable safety boundaries.
- `design/current/interface-language.md` — current UI tone, visual defaults, state/evidence language, and browser-sanity workflow.
- `design/current/component-patterns.md` — current component defaults, page responsibilities, and test expectations.

## Historical design references

- `design/v1-baseline/README.md` — early design-bundle map and traceability note.
- `design/v1-baseline/architecture-data-model.md` — historical stub for early architecture/data-model thinking.
- `design/v1-baseline/rules-and-interaction.md` — historical stub for early rules and interaction thinking.
- `design/v1-baseline/tech-selection.md` — historical stub for early technology-selection thinking.
- `design/v1-baseline/interactive-prototype-and-operation-flow.md` — historical stub for early operation-flow thinking.
- `design/v2-houfeng/design-language.md` — historical stub for the previous visual-language bundle.
- `design/v2-houfeng/component-spec.md` — historical stub for the previous component/page contract bundle.

Historical design stubs explain where old bundles went. Full old text is available through git history when needed for archaeology, but it is not the public quick-start path and should not be treated as a reason to freeze future detail. Use `design/current/` for maintained guidance.

## Evidence and validation

- `operations/ui-preview-and-browser-sanity.md` — records the local preview/browser-sanity workflow and the policy that screenshots remain untracked unless explicitly approved as public README/docs assets.
- `operations/asset-ledger-real-data-validation-readiness.md` — records the active local-sample evidence level and the privacy checklist required before using real inventory data.

## Documentation contribution rules for this early-stage repo

- Keep README and this index concise and public-reader friendly.
- Keep current operator commands verifiable against `.env.example`, `Makefile`, code routes, and checked-in scripts.
- Label evidence level and data source honestly: mock API, local center sample, or real data.
- Do not claim production readiness, package manager support, Kubernetes deployment, containerized agents, automatic upgrades, completed real-data validation, provider account truth, or billing accuracy unless current code/evidence proves it.
- Preserve token and real-data secrecy: enrollment tokens, sync tokens, passwords, SSH keys, provider credentials, session cookies, webhook URLs, and unrelated private notes must not be committed or pasted into public docs.
