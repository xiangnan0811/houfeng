# Research: Official Trellis update path

- Query: How should Houfeng update local Trellis CLI and project integration safely?
- Scope: external and local
- Date: 2026-05-08

## Findings

- Official Trellis docs describe global installation with `npm install -g @mindfoldhq/trellis` and project initialization/update through the `trellis` CLI.
- Official FAQ describes update as two layers: update the global CLI package, then run `trellis update` in the project.
- Official advanced FAQ recommends `trellis update --dry-run` before apply, and `trellis update --migrate` for major/breaking migrations.
- Official docs state protected project paths include `.trellis/workspace/`, `.trellis/tasks/`, `.trellis/.developer`, `.trellis/spec/frontend/`, and `.trellis/spec/backend/`, and that updates create timestamped backups.
- Local Trellis meta guidance says `.trellis/.template-hashes.json` is used by `trellis update` to distinguish template-managed files from local modifications.

## Caveats / Not Found

- Need live npm dist-tag/version data before choosing `latest`, `beta`, or another tag.
- Need local `trellis --version` and `.trellis/.version` before determining whether an update is actually needed.
