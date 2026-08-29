# Docker Compose proxy-mode research

## Repository evidence

- `compose.yaml` currently attaches only `houfeng` to an external network named by the required `HOUFENG_PROXY_NETWORK`, retains the private default network for database and ClamAV access, and publishes no host port.
- `internal/center/deploy/production_compose_static_test.go` freezes that topology, requires `HOUFENG_PROXY_NETWORK` in the env template, and treats the former loopback-port contract as obsolete.
- The release workflow currently publishes and reads back exactly `compose.yaml` plus `compose.env.example`, so adding mode files changes the public release-asset contract.

## Authoritative Docker behavior

- Docker Compose host networking shares the host network stack, supports neither port publishing nor Compose service-name DNS. A host-mode NPM therefore cannot resolve `houfeng` through a user-defined Docker network.
- A service can join its project-private network and an external network at the same time. This remains the correct default for bridge-mode NPM: Houfeng joins NPM's existing network; NPM is not reconfigured for Houfeng.
- Compose profiles only gate services; top-level networks remain active. Profiles cannot make the current required external-network interpolation conditional.
- Compose interpolates each file before merging. A host override cannot remove the current `${HOUFENG_PROXY_NETWORK:?...}` requirement after the fact.
- `COMPOSE_FILE` may select and merge multiple Compose files from `.env`. A common base file plus one thin mode file can therefore make the external-network requirement exist only in shared-network mode.
- Publishing `127.0.0.1:16001:16001` restricts ordinary NAT-mode access to the Docker host, but Docker Engine releases older than 28.0.0 have a documented same-L2 reachability issue for localhost-published ports.

Official references:

- https://docs.docker.com/compose/how-tos/networking/
- https://docs.docker.com/reference/compose-file/interpolation/
- https://docs.docker.com/reference/compose-file/profiles/
- https://docs.docker.com/reference/compose-file/merge/
- https://docs.docker.com/engine/network/port-publishing/

## Candidate approaches

### A. Common base plus two thin mode files (recommended)

- `compose.yaml`: complete common topology; Center stays on the private default network and has no proxy exposure by itself.
- `compose.proxy-network.yaml`: adds the external network, required `HOUFENG_PROXY_NETWORK`, and stable `houfeng` alias.
- `compose.proxy-host.yaml`: adds a loopback-only `127.0.0.1:16001` publication and no external-network requirement.
- `.env` selects the reviewed file pair through `COMPOSE_FILE`; ordinary `docker compose config/up` commands remain unchanged.

Benefits: no large service duplication, genuinely conditional requirements, explicit safe mode selection, and stable upgrades. Costs: two additional release assets and a mode choice in the env template.

### B. Two complete Compose bundles

Publish a shared-network bundle and a host-proxy bundle, each independently runnable.

Benefits: each mode is mechanically simple for operators. Costs: duplicates the full production topology and makes drift across initialization, secrets, authority, processor, and recovery wiring likely.

### C. Always publish loopback and make the external network optional

Keep one file, always bind Center to `127.0.0.1:16001`, and additionally support an external network when configured.

Benefits: fewest assets and no explicit mode selection. Costs: every deployment gains a host port it may not need; conditional external-network attachment remains awkward; Docker Engine before 28 has a documented localhost-publication caveat; bridge-mode users receive a wider surface than necessary.

## Recommended boundary

Use approach A. Keep shared-network mode as the default selection. Treat host-proxy mode as explicit compatibility mode, bind only IPv4 loopback, document NPM upstream `127.0.0.1:16001`, and require Docker Engine 28 or newer unless a separately verified host-firewall rule is part of the deployment contract.
