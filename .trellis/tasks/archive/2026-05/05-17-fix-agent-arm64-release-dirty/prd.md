# fix: agent arm64 release asset dirty metadata

## Goal

Ensure automated agent release assets are built from the clean release tag so both linux/amd64 and linux/arm64 binaries report clean Go VCS metadata.

## Requirements

* The `build-agent-release` target must not make the Git working tree dirty before building the second architecture.
* The generated `sha256sums.txt` must still include both installer-required assets.
* The fix must preserve the installer asset names: `houfeng-agent_<version>_linux_amd64`, `houfeng-agent_<version>_linux_arm64`, and `sha256sums.txt`.

## Acceptance Criteria

* [ ] Building agent assets from a clean checkout produces amd64 and arm64 binaries with `vcs.modified=false`.
* [ ] `sha256sum -c sha256sums.txt` passes for both generated binaries.
* [ ] The release workflow continues to upload all three agent assets.
* [ ] No enrollment token or deployment secret is recorded.

## Definition of Done

* Go release asset build verified locally.
* CI/checks green after PR.
* Release workflow revalidated by a follow-up release or manual equivalent.

## Technical Notes

* `dist/` is not ignored, so writing the first binary there can dirty the checkout before the second `go build` embeds VCS metadata.
* Build both binaries outside the repository working tree, then copy final artifacts into `dist/` before checksumming.
