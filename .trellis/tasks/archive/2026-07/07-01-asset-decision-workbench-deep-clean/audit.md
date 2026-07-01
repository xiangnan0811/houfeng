# Asset decision workbench audit

## Code-path audit

- Automatic group modal:
  - Opens at `groupDetailPanel === 'overview'`.
  - Default cover is already mostly quiet.
  - Problem: `查看详情` sets `groupDetailPanel` to `members`, immediately rendering the full member report and actions.
  - Problem: raw data and save panel are reachable only after entering the same heavy detail layer.

- Manual group modal:
  - Opens at `manualDetailPanel === 'overview'`.
  - Problem: `查看详情` sets `manualDetailPanel` to `members`, immediately rendering the same heavy member cards.
  - Problem: member maintenance, edit, save, add, and raw data are all presented as tabs once any detail is opened.

- Saved record modal:
  - Opens at `recordDetailPanel === 'overview'`.
  - Default cover is quiet.
  - Existing `查看详情` enters execution follow-up. This is acceptable as a single explicit panel, but source review can reopen an automatic group, so the reopened group must obey the same cover/directory rules.

- Template modal:
  - Opens at `templateDetailPanel === 'overview'`, but the modal immediately renders summary facts, lead block, and full panel nav.
  - Problem: it lacks the same quiet cover budget as groups and records.

- Member detail renderer:
  - `renderMemberDecisionCards` creates large report-style cards with member name, role/action badges, summary, four fact boxes, strengths, risks/gaps, evidence chips, and optional actions.
  - This is the main source of the screenshot-like long-report feel after one click.

## Target checks

- Default cover for automatic groups, manual groups, records, and templates must not show member names, raw table labels, save forms, member actions, or detail nav.
- `查看详情` on automatic/manual/template details must first show a detail directory.
- The directory must not show member names, raw tables, save forms, or per-member actions.
- Member panels must show compact decision rows and only appear after explicitly choosing the member entry.
- Existing write payloads for record save, manual group edit/member removal, template creation/status, source review, and VPS decision handling must remain unchanged.
