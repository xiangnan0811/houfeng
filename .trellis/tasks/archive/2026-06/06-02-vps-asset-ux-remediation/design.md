# VPS asset UX remediation design

## Architecture

This task spans PostgreSQL migrations, Go domain/store/handler contracts, React API types, and high-density form UX. Keep the current single-center/PostgreSQL architecture and existing `country`, VPS access, subscription, and monitoring instance resource boundaries.

## Backend Contract

- Add subscription fields `billing_period_unit`, `billing_period_length`, and `renewal_mode`.
- Keep legacy `billing_cycle`, `billing_months`, `auto_renew`, and `auto_renew_cancelled` readable during the transition so existing pages/tests can degrade safely while callers migrate.
- Calculate monthly price from the new period fields using deterministic approximations: month = length, year = length * 12, week = length * 7 / 30, day = length / 30. Store the computed month denominator as needed for compatibility.
- Add validity extension lifecycle action `extend_validity` with reason, new renew date, fee, fee currency, source type, old renew date, and subscription id.
- Validity extension requires exactly one current active subscription for the VPS; it updates that subscription renew date and writes price/date history through the existing renewal timeline path.

## Frontend Contract

- Add shared option helpers for country, currency, payment method, billing period, and renewal mode options.
- VPS access form owns UI-only state for IPv6 enabled and SSH host override; submitted payload remains the existing `ipv4`, `ipv6`, `ssh_host`, `ssh_port`, `ssh_user`.
- Subscription forms share one component for create/edit and submit the new fields plus legacy compatibility fields while backend transition remains in place.
- Monitoring instance create form accepts optional VPS context and completion target. From VPS detail it pre-populates display name, provider, region, city, labels, and note.
- Onboarding drawer reads a source/return query parameter to decide whether its final CTA returns to the VPS detail or stays on monitoring detail.

## UX Rules

- Modal defaults move from tiny CRUD dialogs toward operational forms; use `lg` or `xl` for form-heavy flows.
- Success closes or navigates. Failure stays local. Cancel discards draft. URL-triggered dialogs clean transient query params.
- Fold secondary onboarding explanation by default; commands should be readable and copy-first.

## Compatibility

- Existing data migrates in place with defaults derived from old fields.
- Existing import/visual fixtures can keep old fields; API responses should include new fields after normalization.
- Do not require new external image/icon dependencies for currency or payment labels.
