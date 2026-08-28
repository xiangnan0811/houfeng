# Delivery and release evidence

- Date: 2026-08-28 (Asia/Shanghai)
- Initial release baseline: `v0.77.6` / `da83a96769b618c6e223f71a1d2c6645d54c853b`
- Feature branch: `codex/vps-write-idempotency-hardening`
- Archive branch: `codex/archive-vps-write-idempotency-hardening`

## Batched feature delivery

The externally approved work was committed in logical batches:

- `2740cc8a` — `fix(contracts): make VPS date formats explicit`
- `4780251e` — `feat(store): persist VPS create idempotency receipts`
- `5a6bf2b0` — `feat(api): enforce VPS scoped create idempotency`
- `398bc261` — `feat(web): preserve VPS write ownership across routes`
- `721188b9` — `docs(trellis): record VPS idempotency contracts`
- `72c0d991` — `test(e2e): scope record heading assertions`

Feature PR: <https://github.com/xiangnan0811/houfeng/pull/468>

- Verified head: `72c0d9912b633ff0de410564e4d1ccf39e7cd217`
- Required CI: <https://github.com/xiangnan0811/houfeng/actions/runs/33182939197> — PASS, seven of seven jobs
- Protected squash merge: `080d2c025bf843d193f9d5fb69542af18083918e`
- Post-feature main CI: <https://github.com/xiangnan0811/houfeng/actions/runs/33183499335> — PASS, seven of seven jobs
- Release Please preparation: <https://github.com/xiangnan0811/houfeng/actions/runs/33183499353> — PASS

The first feature PR CI run exposed a deterministic, pre-existing Playwright strict-locator ambiguity in the unchanged record workspace spec. The failure was reproduced locally, fixed with a page-header scope, and the full 133-test Chromium suite passed before the corrected head was pushed. The failed run is retained as diagnostic history: <https://github.com/xiangnan0811/houfeng/actions/runs/33182127639>.

## Release PR and published version

Release PR: <https://github.com/xiangnan0811/houfeng/pull/469>

- Release head: `fed1a072025b5f9a21316ff1e468642a20228124`
- Required CI: <https://github.com/xiangnan0811/houfeng/actions/runs/33183539006> — PASS, seven of seven jobs
- Protected merge: `415de509ca853769fa97d480fd9f473896ba5a55`
- Release Please publication: <https://github.com/xiangnan0811/houfeng/actions/runs/33183993833> — PASS
- Release-after-merge main CI: <https://github.com/xiangnan0811/houfeng/actions/runs/33183993850> — PASS, seven of seven jobs
- Public release: <https://github.com/xiangnan0811/houfeng/releases/tag/v0.78.0>

`v0.78.0` is public, non-draft, and non-prerelease. Its tag and GitHub Release target resolve exactly to `415de509ca853769fa97d480fd9f473896ba5a55`.

## Release assets and signatures

`publish-images` run <https://github.com/xiangnan0811/houfeng/actions/runs/33184005814> passed all jobs: source resolution, credentials check, agent assets, amd64 build, arm64 build, manifest publication/inspection, and deployment-asset publication/verification.

The six public assets are:

- `houfeng-agent_v0.78.0_linux_amd64`
- `houfeng-agent_v0.78.0_linux_arm64`
- `sha256sums.txt`
- `sha256sums.txt.minisig`
- `compose.yaml`
- `compose.env.example`

Independent local verification downloaded the four agent/checksum assets, verified `sha256sums.txt.minisig` with the installer-pinned public key, confirmed trusted comment `houfeng v0.78.0 checksum manifest`, and passed `sha256sum -c` for both binaries. The temporary verification directory was removed after the checks.

## Docker Hub verification

Public registry inspection confirmed that `docker.io/linnea7171/houfeng:v0.78.0`, `:0.78.0`, and `:latest` all resolve to the same OCI index:

- Index digest: `sha256:73772ba18dcbfb37b622117f2fce9d5b4ffa5018541b3c04ee78001912e7e27a`
- linux/amd64: `sha256:c485af5878f978963edbf82c067285ad9a23924cf207abb49e96f26d6af04795`
- linux/arm64: `sha256:a80bfdbca988728a9e0e89f698530ecbba8cb9e403f9798f6926fad636ef2cd7`
- Each platform image also has a published provenance attestation in the index.

## Cleanup boundary

The feature worktree remained clean and recoverable until the feature merge, release PR merge, published tag, signed assets, multi-architecture images, and release-after-merge main CI were all verified. Task archival and journal recording are delivered through the separate protected archive branch/PR. After that merge, local main is fast-forwarded to `origin/main`, the two task worktrees and merged local/remote branches are removed, stale worktree/ref metadata is pruned, and the primary checkout is verified clean for the next task.

This evidence contains no credentials, DSN, idempotency key, request body, note/details content, raw internal error, or private signing material.
