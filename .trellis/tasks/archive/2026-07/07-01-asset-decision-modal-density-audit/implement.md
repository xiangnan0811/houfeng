# Asset decision modal density implementation plan

## Preconditions

- Branch: `ux/asset-decision-modal-density-audit`.
- Hooks enabled with `sh scripts/setup-git-hooks.sh`.
- Task remains planning until this document and `design.md` are reviewed and approved.
- Before editing production code, run `python3 ./.trellis/scripts/task.py start 07-01-asset-decision-modal-density-audit`.

## Implementation Order

### 1. Visual companion mockup

- Create a local ignored mockup under `.superpowers/brainstorm/asset-decision-modal-density/`.
- Show at least:
  - current-problem style: long stacked modal.
  - target style: cover -> directory -> single compact task panel.
  - automatic group members panel target.
  - saved record directory target.
- Use existing product language and dark-first visual tone.
- Use browser screenshot/inspection for the mockup before implementation.

### 2. RED tests

Update `web/src/pages/AssetDecisionsPage.test.tsx` before production code:

- Automatic cost group:
  - After `members`, assert no report-stack markers:
    - no `asset-decision-detail__summary`-style summary text expectations such as full evidence summary in visible group-level summary.
    - no bottom `决策组成员对比` table unless `数据底稿` selected.
    - no all-member edit form.
    - no long facts strings such as `服务 2 · 域名 1 · Target`.
- Automatic save panel:
  - Click `保存记录面板`.
  - Assert record basics visible.
  - Assert all member edit controls are not expanded by default.
  - Preserve existing submit payload by expanding/editing only if needed.
- Manual group:
  - `members` panel uses compact rows and does not show edit/add/save/raw content.
  - `edit`, `add`, `save`, `raw` each show only their own task.
- Template:
  - `create` no long "重新读取当前事实" explanation.
  - `members` no large explanatory heading/body.
  - `status` confirmation copy only appears after action.
- Saved record:
  - `查看详情` opens directory first.
  - `execution` panel visible only after choosing execution.
  - `members` and `raw` remain separate.
  - Existing PATCH payload tests still pass after navigating through directory.

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Confirm failures are caused by current over-dense behavior.

### 3. Production UI changes

Edit `web/src/pages/AssetDecisionsPage.tsx`:

- Add `directory` to `RecordDetailPanel`.
- Change saved record `查看详情` to `setRecordDetailPanel('directory')`.
- Add record directory entries for execution, source, members, raw.
- Stop rendering global `asset-decision-detail__summary` for every non-overview panel; use compact task headers instead.
- Replace `renderMemberDecisionRows` with a stricter compact row renderer:
  - no full facts string by default.
  - no multi-chip evidence dump.
  - one summary line only.
- Add compact save member review:
  - default rows show member, role/action, reason status.
  - expose individual reason/role/action editing without opening every row as a full form.
  - preserve `recordDraft` state and submit payload.
- Simplify manual group edit/add/template create/status copy.
- Simplify record execution board:
  - keep status PATCH form and quick actions.
  - use compact execution rows/cards.
  - keep detailed member table in members/raw panels.

### 4. CSS changes

Edit `web/src/index.css`:

- Add/adjust:
  - `.asset-decision-task-panel`
  - `.asset-decision-task-panel__header`
  - `.asset-decision-decision-strip`
  - `.asset-decision-decision-strip__row`
  - `.asset-decision-save-brief`
  - `.asset-decision-save-member`
  - `.asset-decision-record-directory`
- Reduce heavy nested card padding where retained.
- Ensure mobile max width:
  - group/manual/template/record modal width `100%` at small viewport.
  - no body/document horizontal overflow.
  - raw tables keep internal min-width and scroll.
- Remove unused heavy CSS only after `rg` proves no production caller remains.

### 5. GREEN tests

Run in order:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
```

### 6. Browser sanity

Start local frontend/mock API as needed. Verify:

- Desktop `1440x1000`:
  - automatic cost group
  - automatic non-cost group
  - manual group
  - template
  - saved record -> source review -> reopened group
- Mobile `390x900` same representative paths.

For each, inspect:

```js
({
  docOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
  bodyOverflow: document.body.scrollWidth > document.body.clientWidth,
  dialogTextLength: document.querySelector('[role="dialog"]')?.innerText.length
})
```

Screenshots are local-only evidence and must not be committed unless explicitly requested.

### 7. Finish

- Run Trellis task validation.
- Run `git diff --check`.
- Update `.trellis/spec/web/component-conventions.md` if this produces a reusable rule about secondary modal panel density.
- Commit implementation.
- If user asks for delivery, follow PR + CI + Release Please + image publishing flow.

## Risk Checks

- Do not change write payload contracts.
- Do not remove any existing action; only move it behind clearer task panels.
- Do not introduce new dependencies.
- Do not make raw tables disappear; they remain explicit low-priority entries.
- Do not mark complete until browser sanity covers both desktop and mobile.
