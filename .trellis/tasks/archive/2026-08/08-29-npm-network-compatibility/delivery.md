# Delivery evidence

## Work and protected delivery

- Planning commit: `d61d4d37b6c4f237f3ad4dc6ff2771c8d9da47b1`
- Work commit: `1fa3eb33af309f9b503f0a18c5e27d8d0459817a`
- Feature PR: https://github.com/xiangnan0811/houfeng/pull/471
- Feature merge commit: `123f85656b2e520b048c6486cbafd028a0c57e21`
- Release PR: https://github.com/xiangnan0811/houfeng/pull/472
- Release merge commit: `5eceb02ffda6706a51928379ac07e51faf18e4ff`

Both pull requests were merged through the protected branch path after all seven required checks passed. The post-release-merge main CI run `33236459595` also completed successfully with Go, Web, browser, Docker image, and all three PostgreSQL 16 catalog jobs green.

## Release

- Release: https://github.com/xiangnan0811/houfeng/releases/tag/v0.79.0
- Publish run: https://github.com/xiangnan0811/houfeng/actions/runs/33236465908
- Tag commit: `5eceb02ffda6706a51928379ac07e51faf18e4ff`
- Image: `docker.io/linnea7171/houfeng:v0.79.0`

The publish workflow completed successfully. The public image manifest contains `linux/amd64` and `linux/arm64`.

## Public deployment asset verification

The release exposes exactly one of each required deployment asset while preserving the unrelated agent assets:

- `compose.yaml`
- `compose.proxy-network.yaml`
- `compose.proxy-host.yaml`
- `compose.env.example`

All four assets were downloaded into a fresh temporary directory. The three Compose files were byte-identical to the tagged sources. The env asset was byte-identical after applying the release workflow's expected `HOUFENG_IMAGE=docker.io/linnea7171/houfeng:v0.79.0` substitution.

Structured rendering of the downloaded assets passed for both supported modes:

- shared-network mode: exactly eight services; Center joins only the default and selected external proxy networks, has alias `houfeng`, and publishes no host port;
- host-proxy mode: exactly eight services; Center remains only on the default network and publishes exactly `127.0.0.1:16001 -> 16001/tcp`, with no external proxy network.

## Local quality evidence

- `GOTOOLCHAIN=go1.26.2 make verify-go`: pass
- focused deployment and admin tests: pass
- both structured Compose renders and the missing-network fail-closed case: pass
- workflow YAML parsing and all deployment-assets shell bodies: pass
- `git diff --check`: pass
- full independent Trellis review: Critical 0, Important 0, unresolved Minor 0

`actionlint` was not installed locally; equivalent workflow YAML parsing, shell syntax checks, static contracts, and GitHub Actions execution all passed.
