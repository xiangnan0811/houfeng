# Houfeng documentation index

This index keeps the current public/operator path separate from design references and local validation workflows. Completed roadmap, release-gate, historical archive, and one-off process/evidence logs have been removed from tracked docs rather than kept as in-repository archive copies.

## Start here

- `../README.md` — public project overview, quick start, components, verification commands, and documentation map.
- `deploy/local-and-systemd.md` — canonical deployment guide for local, Docker Compose center, systemd center installs, and one-command Linux agent onboarding.
- `operations/v1-smoke-run.md` — fresh-install smoke run. The primary onboarding path is the center-generated one-command installer.

## Current operator guides

- `deploy/local-and-systemd.md` — build artifacts, center environment, Docker Compose center deployment (`houfeng` service on `127.0.0.1:16001`, `./data/postgres/`, file-logging follow-up), authentication, reverse proxy/TLS notes, systemd examples, generated install commands, checksum verification, and manual troubleshooting fallback.
- `deploy/systemd/houfeng-center.service` — example center systemd unit.
- `deploy/systemd/houfeng-agent.service` — example agent systemd unit for manual installs and reference.
- `operations/v1-smoke-run.md` — live PostgreSQL smoke path for center/auth/node/agent/target/probe/incident/event checks.
- `operations/v2-visual-evidence.md` — active frontend preview, browser sanity, local screenshot policy, and protected-route mock/local-center data-source rules.
- `operations/asset-ledger-real-data-validation-readiness.md` — non-sensitive sample, import dry-run/import workflow, authenticated browser sanity, and real-data privacy checklist.
- `operations/asset-ledger-local-sample.json` — fake local sample for Asset Ledger dry-run/import validation.

## Current design references

- `design/v2-houfeng/design-language.md` — active visual language.
- `design/v2-houfeng/component-spec.md` — active component reference.
- `design/v1-baseline/README.md` — V1 baseline map and traceability note.
- `design/v1-baseline/architecture-data-model.md` — retained data/model baseline.
- `design/v1-baseline/rules-and-interaction.md` — retained rule and interaction baseline.
- `design/v1-baseline/tech-selection.md` — retained technology baseline.
- `design/v1-baseline/interactive-prototype-and-operation-flow.md` — retained operation-flow baseline.

The v1 baseline is useful for understanding why the current model exists, but it is not the public quick-start path and should not be treated as a reason to freeze every future detail. Visual authority is v2-houfeng.

## Evidence and validation

- `operations/v2-visual-evidence.md` — records the active local preview/browser-sanity workflow and the policy that screenshots remain untracked unless explicitly approved as public README/docs assets.
- `operations/asset-ledger-real-data-validation-readiness.md` — records the active local-sample evidence level and the privacy checklist required before using real inventory data.

## Documentation contribution rules for this early-stage repo

- Keep README and this index concise and public-reader friendly.
- Keep current operator commands verifiable against `.env.example`, `Makefile`, code routes, and checked-in scripts.
- Label evidence level and data source honestly: mock API, local center sample, or real data.
- Do not claim production readiness, package manager support, Kubernetes deployment, containerized agents, automatic upgrades, completed real-data validation, provider account truth, or billing accuracy unless current code/evidence proves it.
- Preserve token and real-data secrecy: enrollment tokens, sync tokens, passwords, SSH keys, provider credentials, session cookies, webhook URLs, and unrelated private notes must not be committed or pasted into public docs.
