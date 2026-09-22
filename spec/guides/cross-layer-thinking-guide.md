# Cross-Layer Thinking Guide

> **Purpose**: Think through data flow across layers before implementing.

---

## The Problem

**Most bugs happen at layer boundaries**, not within layers.

Common cross-layer bugs:
- API returns format A, frontend expects format B
- Database stores X, service transforms to Y, but loses data
- Multiple layers implement the same logic differently

---

## Before Implementing Cross-Layer Features

### Step 1: Map the Data Flow

Draw out how data moves:

```
Source → Transform → Store → Retrieve → Transform → Display
```

For each arrow, ask:
- What format is the data in?
- What could go wrong?
- Who is responsible for validation?

### Step 2: Identify Boundaries

| Boundary | Common Issues |
|----------|---------------|
| API ↔ Service | Type mismatches, missing fields |
| Service ↔ Database | Format conversions, null handling |
| Backend ↔ Frontend | Serialization, date formats |
| Component ↔ Component | Props shape changes |

最小权限数据库边界还要区分“catalog grant 正确”和“生产 SQL 可执行”：`ON CONFLICT` target、`RETURNING`、row lock、trigger 或函数都可能引入隐式权限。只要 SQL 依赖 least-privilege runtime role，就应使用 production repository + direct runtime session 执行代表性 DML；fake transaction 与 catalog-only 断言不能替代这项证据。具体合同由对应 backend database spec 定义。

For Go ↔ TypeScript JSON boundaries, explicitly test empty collections: a nil Go slice is serialized as `null`, not `[]`. A handwritten TypeScript array type does not validate runtime JSON, so decide whether the backend guarantees non-nil slices or the owning frontend boundary normalizes nullable collections before array operations.

### Step 3: Define Contracts

For each boundary:
- What is the exact input format?
- What is the exact output format?
- What errors can occur?

---

## Common Cross-Layer Mistakes

### Mistake 1: Implicit Format Assumptions

**Bad**: Assuming date format without checking

**Good**: Explicit format conversion at boundaries

### Mistake 2: Scattered Validation

**Bad**: Validating the same thing in multiple layers

**Good**: Validate once at the entry point

### Mistake 3: Leaky Abstractions

**Bad**: Component knows about database schema

**Good**: Each layer only knows its neighbors

### Mistake 4: Configured policy reaches only one trigger

**Bad**: A periodic worker reads persisted policy, while an event-driven or post-commit hook calls the same evaluator with an optional argument or silent default.

**Good**: Enumerate every public trigger, resolve one explicit validated policy at each trigger, and make missing/invalid policy fail closed. Add a regression that invokes every public trigger with the same non-default persisted value.

If a bounded downstream query derives its limit from an upstream ingress maximum, the bound is a cross-layer contract: enforce every contributing ingress invariant (count, grouping/key identity, and duplicate semantics) at the actual HTTP/service boundary and test the rejection path before relying on the limit in SQL.

---

## Checklist for Cross-Layer Features

Before implementation:
- [ ] Mapped the complete data flow
- [ ] Identified all layer boundaries
- [ ] Defined format at each boundary
- [ ] Decided where validation happens
- [ ] Enumerated periodic, request-driven, post-commit, retry and reconciliation entry points that can invoke the same policy
- [ ] For a query bound derived from ingress limits, identified the count, grouping/key and duplicate invariants that make the bound true
- [ ] For least-privilege SQL, identified implicit read/write/execute requirements and the direct-runtime test that proves the production statement is executable

After implementation:
- [ ] Tested with edge cases (null, empty, invalid)
- [ ] For every Go slice field, verified the actual JSON shape for both nil and empty values (`null` versus `[]`)
- [ ] Verified error handling at each boundary
- [ ] Checked data survives round-trip
- [ ] Called every public policy entry point with one persisted non-default value and proved none can silently use a fallback
- [ ] Proved derived query bounds at both ends: ingress rejects violating payloads and the production query stays fail closed if legacy/direct writers bypass the boundary
- [ ] For ACL-sensitive persistence, ran production SQL as the runtime role and verified both intended privileges and forbidden table/column grants

---

## When to Create Flow Documentation

Create detailed flow docs when:
- Feature spans 3+ layers
- Multiple teams are involved
- Data format is complex
- Feature has caused bugs before
