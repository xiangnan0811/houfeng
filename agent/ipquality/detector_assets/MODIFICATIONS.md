# IPQuality local detector fork

Upstream: https://github.com/xykt/IPQuality

Pinned commit: `2384a67c756eb35231f5982b34731e522be3653e` (`v2026-09-16`).
Local fork revision: **1**. Detector JSON schema revision: **1**.

The complete upstream AGPL-3.0 license is in `LICENSE`. `upstream-ip.sh.txt` is the corresponding original source at the pinned commit, retained as inert audit/source material, never executed. `iso3166.json` is the original `ref/iso3166.json` from that same commit, retained locally rather than downloaded at runtime. `SHA256SUMS` records the actual source and asset bytes. Upstream tracking-cookie literals in the inert source are not used; no upstream `cookies.txt` is distributed.

## Adapted modules and audited differences

`detector.sh` is the modified, executable extraction. Its functions correspond to upstream `ip.sh` lines 700–826 (`db_maxmind` / `db_ipinfo`), 1455–1574 (`MediaUnlockTest_Netflix`, `MediaUnlockTest_YouTube_Premium`, `MediaUnlockTest_PrimeVideo_Region`) and 1625–1699 (`OpenAITest`). The original field paths, title samples, territory lookup, country-refusal branches and anonymous cookie-requirements exchange remain identifiable in these functions. The surrounding interactive application is not executed.

- Removed startup networking, shell installation/upgrades, menus, telemetry, statistics, advertisements, uploads, background progress processes, DNS-based unlock typing, map/DMS generation, unrelated providers/services and independent IP/family discovery.
- Removed curl/dig/bc/text-tool dependencies. Production needs only Bash >=4, jq and the Agent itself. No dependency is automatically installed. All HTTP goes through the same Agent's closed `ip-quality-fetch` entrypoint, before configuration/token loading. No arbitrary URL/header/config path is accepted.
- Replaced body command substitution, regex scraping and dynamically assembled jq programs with bounded private response files, the existing safe static HTML/JSON lexer, explicit business renderer extraction and static jq programs with data passed via `--arg` / `--argjson`. This does not execute page JavaScript.
- Netflix checks two pinned non-original title identities and gives explicit country denial precedence over Android title metadata. Positive content requires a visible heading and nonempty page content. Only two explicit denials cause the fixed original-title control request. Original positive means `partial/originals_only`; an unknown control stays unknown. Missing denial text is not positive evidence.
- YouTube only uses trusted `ytInitialData` / `ytcfg` objects, the explicit Premium header/offer-card subtree, and visible country-refusal text. Arbitrary marketing strings, comments or unrelated objects cannot establish access. The upstream tracking/session cookies are removed; consent/challenges remain unknown.
- Prime requires a branded complete page and actual `currentTerritory` business data, with explicit denial first. A missing field is unknown rather than blocked. The decompressed limit is 8 MiB (other modules 2 MiB); an incomplete/over-limit body is never classified.
- OpenAI requires a typed successful cookie response for positive evidence. Explicit unsupported-country evidence can conflict with it; generic 403, `type:dc`, VPN help, maintenance and otherwise unrecognized ChatGPT/OpenAI-branded HTML stay endpoint-unknown. No positive iOS-page signature has been established. Removed favicon and trace fallback requests. No claim of account login, paid playback or actual App access is made.
- Geo keeps MaxMind's real registered-country field separate from IPinfo's abuse-contact country. IPinfo is a conditional fallback and must return the exact queried IP. Returned MaxMind IP fields, when present, must agree. Missing privacy flags are never manufactured as false. The Go adapter records anonymous provenance and the abuse-contact comparison basis separately, without putting it into registered-country.
- The Agent validates the versioned JSON, canonical target/family, module, endpoint set, statuses, bounded fields and provenance. The four service rows use `evidence_kind=ipquality_script`, per-row probe revision 5 and no fabricated legacy checks; the other three services remain unchanged revision-4 producers.

## Execution and network boundaries

The worker uses a private 0700 cwd and fixed exclusive 0600 files, closed stdin, Bash `--noprofile --norc`, a minimal environment and trusted dependency locations `/usr/bin` and `/bin`. No Agent secret or inherited proxy/BASH_ENV/ENV variable is forwarded. stdout is limited to 128 KiB and stderr to 64 KiB; stderr/body/token contents never enter reports. A separate process group is killed on cancellation/timeout/output overflow and after completion, before removing worker files. Module budgets and shared pools are owned by the collector; they do not extend the parent deadline.

Fetch uses fixed HTTPS GET endpoints and English headers, explicit tcp4/tcp6 dialing, no environment proxy, public-address validation of resolved dial targets, same-address validation/dialing, bounded redirect counts and per-endpoint hostname allowlists. The complete decompressed body is streamed to an exclusive file with limit+1 completeness checking; only fixed static extraction files are consumed by the shell.

This is **not an OS sandbox**. Its boundary is reviewed fixed source, closed inputs, bounded subprocess execution and minimal credentials/environment. It does not claim to prevent arbitrary locally modified code from networking.

## Corresponding source and distribution

The Go embed includes these assets in the Agent. The corresponding source is this directory plus `../detector.go`, `../detector_fetch.go`, relevant static parsing in `../services.go` / `../service_http.go`, `../../../cmd/houfeng-agent/main.go`, and the repository's build materials. Source-bundle packaging must retain them with this license and provenance, under the existing release checksum/signature workflow.

Houfeng's original code is explicitly licensed by its owner under AGPL-3.0-only; see the repository-root `LICENSE` and README. Third-party code retains its own licenses and notices. A subprocess boundary is not a license exemption. Public releases must retain the corresponding source, build materials and notices, and operators of modified network-facing versions must offer users the corresponding source of the version actually running. Publication and deployment still require their own operational authorization.
