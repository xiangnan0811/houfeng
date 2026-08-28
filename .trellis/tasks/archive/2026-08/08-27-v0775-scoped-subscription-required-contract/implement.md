# Implement: scoped subscription required contract

## TDD sequence

1. **RED — required presence at the real HTTP door**
   - Add a complete valid scoped JSON fixture helper in `subscriptions_test.go`.
   - Add table-driven tests that delete each of the eight required keys and assert `400` plus zero repository calls.
   - Add table-driven tests that set each required key to JSON null with the same assertions.
   - Run the new focused tests and record the expected failure for currently accepted missing fields.

2. **GREEN — presence-aware scoped request**
   - Change only `vpsSubscriptionCreateRequest` to existing `subscriptions.Optional*` wrappers.
   - Mark manifest-required fields with `required:"true"`.
   - Add explicit presence validation and a single mapping into the unchanged domain `CreateInput`.
   - Keep normalization, validation, idempotency, repository, and collection-handler flow intact.
   - Run the focused required/null tests to green.

3. **RED/GREEN — nullable and explicit zero semantics**
   - Add null cases for optional non-nullable fields and accepted null cases for both dates.
   - Add a repository-capture case for zero price, false booleans, blank payment method, and blank note.
   - Make only the minimum mapping adjustment needed for exact semantics and run focused tests.

4. **Contract test adaptation**
   - Extend `vps_subscription_create_contract_test.go` type/required/nullability extraction for optional wrappers and the required tag.
   - Minimally update `web/src/lib/vpsSubscriptionCreateContract.test.ts` source parsing for wrapper spellings.
   - Preserve negative drift assertions for field type, requiredness, and nullable date semantics.

4a. **RED/GREEN — reject parser false greens**
   - Add TypeScript and Go-mirror direct negatives for `number | string`, `string | undefined`, unknown, and empty union members; record the expected keyword-parser RED.
   - Add direct REDs for unknown named Go DTO primitives and missing/null manifest `required` / `nullable` keys.
   - Classify exact union members into one JSON primitive kind plus optional `null`; reject everything else.
   - Keep current `BillingPeriodUnit | string`, `RenewalMode | string`, and nullable date shapes green without an AST dependency.

4b. **RED/GREEN — reject omitted wire fields and named pointer aliases**
   - Add Go/TS synthetic structs for missing tag, empty tag, empty name with options, and exact `json:"-"`; the first three fail and only dash is ignored.
   - Add a Go reflection negative for an unknown named pointer whose element is `subscriptions.Date`; reject before generic pointer unwrapping.

4c. **RED/GREEN — reject anonymous promotion and prove scoped unknown-field decoding**
   - Add Go/TS synthetic DTO negatives for exported and unexported-type anonymous embedding. Reject every anonymous field unless its tag is exactly `json:"-"`; do not silently skip promoted members.
   - Strengthen the real scoped `status` rejection with a valid `Idempotency-Key`, exact `error: "invalid json"` response, and zero idempotent repository calls/key capture.

4d. **RED/GREEN — validate aliases, declaration suffix, and inline comments**
   - Add TS and Go-mirror synthetic sources where `BillingPeriodUnit` / `RenewalMode` contain `number` or `undefined`; record the current name-only allowlist RED.
   - Parse approved alias definitions from the same source and accept only non-empty string-literal members before classifying the DTO field.
   - Add `export type ... = { ... } & { debug?: string }` negatives in both mirrors and reject every closing-brace suffix except whitespace/one semicolon.
   - Add a Go-source synthetic anonymous embedding with a trailing `//` comment; strip the comment before declaration classification or reject it explicitly so it cannot be silently omitted.
   - Add direct quality REDs for block-comment DTO/alias shadows, `notjson` / `notrequired`, unsupported named primitive direct rejection, and quoted alias literals containing escapes/embedded pipes; keep the fix bounded and symmetric.

4e. **RED/GREEN — close multiline and block-comment bypasses**
   - Add TS/Go direct REDs for `}\n& { debug?: string }` and for both approved aliases widened by continuation-line `| number` / `| undefined`.
   - Reject multiline object/alias continuation after whitespace/comment trivia without adding an AST dependency.
   - Strip closed block comments outside raw Go tags before anonymous-field classification; preserve raw-tag comment markers and reject unterminated/multiline block comments.

5. **Existing fixture and spec reconciliation**
   - Add all required keys to existing scoped success/replay/reuse/repository-failure fixtures; do not change collection fixtures.
   - Document runtime enforcement and missing/null/explicit-zero tests in `.trellis/spec/backend/subscription-cost-center.md`.

## Focused commands

```bash
go test ./internal/center/http/handlers -run 'TestVPSSubscriptions.*(Required|Null|Zero|Replay|Reuse)' -count=1
go test ./internal/center/http/handlers -run 'TestVPSSubscriptionCreateRequestMatchesTypeScriptDTO' -count=1
env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  npm --prefix web run test -- --run src/lib/vpsSubscriptionCreateContract.test.ts
go test ./internal/center/http/handlers -count=1
```

Test names may follow local naming conventions, but the behavior matrix and real-handler boundary are mandatory.

## Review gates

- No truthiness-based required check.
- No repository call on decode/presence failure.
- No behavioral change to collection create.
- No source-parser redesign or new generated artifact.
- No keyword-containment fallback that accepts extra primitive kinds or `undefined`.
- Manifest semantics, struct tags, runtime validation, and tests agree exactly.
- Exported fields without explicit usable JSON names cannot disappear from either mirror; unknown named pointers cannot bypass the exact type allowlist.
- Anonymous embedding cannot disappear from either mirror unless explicitly tagged `json:"-"`; the `status` unknown-field test must reach strict JSON decode rather than pass on a missing idempotency key.
- Approved TypeScript aliases are trusted only after their definitions pass a string-literal-only check; intersections/object suffixes and trailing-comment anonymous embeddings cannot disappear from a bounded source mirror.
- Only unique live DTO/alias markers outside comments/strings count; raw struct-tag keys are exact, unsupported named Go types reject directly, and alias tokenization is quote/escape aware.
- Closing-brace and alias validation cannot stop at the first line; later significant `&` / `|` continuations fail closed, and inline block comments cannot turn anonymous Go fields into ignorable lowercase declarations.
