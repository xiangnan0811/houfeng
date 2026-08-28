# Design: scoped subscription required contract

## Request boundary

Change only `vpsSubscriptionCreateRequest` in `internal/center/http/handlers/subscriptions.go`:

```go
type vpsSubscriptionCreateRequest struct {
    Price              subscriptions.OptionalFloat  `json:"price" required:"true"`
    Currency           subscriptions.OptionalString `json:"currency" required:"true"`
    BillingCycle       subscriptions.OptionalString `json:"billing_cycle" required:"true"`
    BillingMonths      subscriptions.OptionalInt    `json:"billing_months" required:"true"`
    BillingPeriodUnit  subscriptions.OptionalString `json:"billing_period_unit"`
    BillingPeriodLength subscriptions.OptionalInt   `json:"billing_period_length"`
    StartedAt          subscriptions.OptionalDate   `json:"started_at"`
    RenewAt            subscriptions.OptionalDate   `json:"renew_at"`
    AutoRenew          subscriptions.OptionalBool   `json:"auto_renew" required:"true"`
    AutoRenewCancelled subscriptions.OptionalBool   `json:"auto_renew_cancelled" required:"true"`
    RenewalMode        subscriptions.OptionalString `json:"renewal_mode"`
    PaymentMethod      subscriptions.OptionalString `json:"payment_method" required:"true"`
    Note               subscriptions.OptionalString `json:"note" required:"true"`
}
```

Exact field spelling/order follows the existing manifest and source, not this illustrative alignment.

## Decode and mapping

The existing optional scalar unmarshallers reject JSON null, which gives non-nullable fields an early `400`. `OptionalDate` retains the distinction between missing and explicit null and maps both accepted cases to the appropriate optional domain pointer.

Add one explicit `validateRequired` / `toCreateInput` boundary that checks `.Set` for the eight manifest-required fields, then copies `.Value` into the unchanged `subscriptions.CreateInput`. Do not check truthiness: zero, false, and empty string are present values. After mapping, keep the existing normalization and domain validation order.

Optional non-nullable wrappers are not required, but their unmarshallers reject explicit null. Their zero value when absent preserves current defaulting. The collection handler continues to decode directly into its existing `CreateInput` path.

## Contract alignment

`vps_subscription_create_fields.json` remains the semantic wire manifest. Update the Go contract test so:

- wrapper types map to `number`, `string`, `boolean`, or `date`;
- nullability comes from wrapper semantics (`OptionalDate` nullable, scalar wrappers non-nullable);
- requiredness comes from `required:"true"`, not pointer/non-pointer inference;
- ordered names still match the manifest and TypeScript DTO.

Keep the source-text helper bounded, but parse TypeScript union members exactly. Reject `undefined`, empty/unknown members, and unions containing more than one JSON primitive kind. Permit the current string aliases (`BillingPeriodUnit`, `RenewalMode`) alongside `string` only after locating exactly one live same-source definition outside comments/strings and proving every member is a non-empty quoted string literal; widening either alias to `number`, `undefined`, another alias, or an empty member fails closed in both mirrors. Quote/escape-aware splitting preserves embedded pipes. Any multiline alias `&` / `|` continuation is fail closed rather than validating only the first line. DTO target markers use the same unique-live rule. Treat `null` only as nullability and preserve `string | null` as the date wire shape. Validate the suffix on the object type's closing-brace line and, when no semicolon terminates it, skip whitespace/comment trivia and reject a later significant `&` / `|`. Both mirrors require every exported field to declare an explicit non-empty JSON name and ignore only exact `json:"-"`; missing/empty tags are wire-surface drift because `encoding/json` otherwise exposes the Go field name. The TS Go-source mirror removes closed line/block comments outside raw tags before counting declaration tokens, preserves comment markers inside raw tags, rejects unterminated/multiline block comments, then reads exact space-delimited raw-tag keys so `notjson` / `notrequired` cannot impersonate the contract keys. Go reflection classification must use exact supported built-in/wrapper types, directly reject unknown named primitives, reject unknown named pointers before `Elem()`, and preserve manifest presence for `required` and `nullable`. Do not move runtime enforcement into the helper and do not expand into AST/parser architecture work.

Because the bounded mirrors do not recursively implement `encoding/json` promotion, they reject every anonymous embedded field before ordinary exported/unexported filtering unless its tag is exactly `json:"-"` with no options. This includes unexported anonymous struct types whose exported members could otherwise be promoted. Ordinary non-anonymous unexported fields remain ignorable.

## Failure and side-effect contract

All decode/presence failures occur before idempotency execution and repository create. Handler tests use a repository spy to prove zero calls. The scoped `status` negative supplies a valid key and full request, then asserts the exact `error: "invalid json"` body plus zero create calls/key capture, so a missing-key 400 cannot false-green strict decoding. Successful requests retain the existing scoped VPS ID binding, normalization, validation, idempotency replay, and response shape.

## Tests

Use table-driven subtests against the real `VPSSubscriptions` handler:

1. remove one required key at a time;
2. replace one required value with null at a time;
3. null each optional non-nullable field;
4. accept null dates;
5. capture an exact full zero/false/empty input;
6. keep existing replay/reuse/failure cases by making their fixtures contract-complete.
7. directly reject `number | string`, `string | undefined`, unknown, and empty union members in both parser implementations.
8. directly reject unknown Go named primitive DTO types and missing/null manifest semantic boolean keys.
9. directly reject exported Go fields with missing/empty/empty-name tags in both mirrors, accept only exact dash omission, and reject an unknown named pointer whose element is an otherwise allowed date type.
10. directly reject exported and unexported-type anonymous embedding in both mirrors, allowing only exact `json:"-"` omission.
11. send scoped `status` with a valid idempotency key and assert exact invalid-json response plus zero idempotent repository calls.
12. parse the real alias definitions and synthetic `number` / `undefined` widenings in both mirrors; only pure string-literal aliases pass.
13. reject a synthetic `} & { debug?: string }` suffix in both object parsers, and reject a trailing-comment anonymous Go embedding in the TS source mirror.
14. reject block-comment DTO/alias marker shadows and near-miss struct-tag keys; directly reject unsupported named Go primitives, while accepting escaped/embedded-pipe non-empty string alias literals.
15. reject later-line object `&` / `|` continuations and both approved aliases widened on continuation lines in both mirrors; reject block-comment anonymous Go embeddings in the TS mirror while preserving raw-tag comment markers.
