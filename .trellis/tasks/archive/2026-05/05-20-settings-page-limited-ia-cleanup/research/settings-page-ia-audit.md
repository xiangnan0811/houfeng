# Research: SettingsPage limited IA cleanup audit

- **Query**: Research SettingsPage limited IA cleanup scope. Goal is frontend-only IA cleanup, not behavior/API changes. Hard frozen contracts: do not alter Telegram/Feishu token masking, unchanged bot_token omit payload behavior, runtime delivery toggles, global save payload semantics, backend/API/data model, notification delivery behavior.
- **Scope**: internal
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/SettingsPage.tsx` | Route/page container: loads settings, builds form state, validates/publishes global update payload, owns tabs, channel modal, active/expanded notification channels, and save feedback. |
| `web/src/pages/SettingsPage.test.tsx` | Regression tests for settings load, tab content, Telegram masking/token omission, runtime delivery toggles, JSON preview, validation, and dismissed channel modal drafts. |
| `web/src/pages/settings/ThemeSettingsSection.tsx` | Theme preset/mode local-browser section. |
| `web/src/pages/settings/FrequencyDefaultsSection.tsx` | Host sample and Probe default frequency controls. |
| `web/src/pages/settings/TelegramSettingsSection.tsx` | Telegram token/chat/runtime section, token masked-status display, expanded/collapsed wrapper support, modal wrapper support. |
| `web/src/pages/settings/FeishuSettingsSection.tsx` | Feishu webhook/enabled section, expanded/collapsed wrapper support, modal wrapper support. |
| `web/src/pages/settings/IncidentDefaultsSection.tsx` | Global incident timing, notification toggles, and metric threshold form. |
| `web/src/pages/settings/OverrideRulesSection.tsx` | Three override JSON textareas with collapsible valid-JSON previews. |
| `web/src/pages/settings/RetentionPolicySection.tsx` | Four retention-day inputs and retention behavior explanation. |
| `web/src/pages/settings/SectionIntro.tsx` | Small helper for section explanatory copy, currently inline-styled. |
| `web/src/pages/settings/types.ts` | Settings form-state shape used by page and section components. |
| `web/src/styles/pages.css` | Settings-related CSS: channel option, settings page rhythm/actions/form grids/fieldsets, JSON preview, responsive settings grid/actions. |
| `web/src/lib/types.ts` | Settings API record/update/input types; fields mirror backend snake_case and include Telegram/Feishu/incident/override/retention contracts. |
| `web/src/lib/api.ts` | `getSettings()` and `updateSettings()` API helpers for `/api/settings`. |
| `.trellis/spec/web/component-conventions.md` | Page/component layering, API-client-only data access, PageState/Modal/Drawer conventions, oversized SettingsPage known gap. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS + BEM + token rules; inline business spacing/style is forbidden and Settings inline-style debt is explicitly known. |
| `.trellis/spec/web/state-and-data.md` | API/data contract discipline: request helpers, snake_case types, local state conventions, no direct page fetch. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification commands, testing patterns, and page-test expectations. |
| `docs/design/v2-houfeng/component-spec.md` | Active SettingsPage visual/IA template: hero, ordered DetailSections, bottom unified save. |
| `.trellis/tasks/archive/2026-05/05-20-continue-remaining-page-information-architecture-optimization/research/remaining-pages-ia-audit.md` | Prior ranking that identified SettingsPage limited cleanup as medium-value/medium-risk and warned to preserve token/save/runtime semantics. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-remaining-page-information-architecture/research/design-patterns-for-page-ia.md` | Reusable page IA patterns: page job, DetailSection, Drawer/Modal, PageState, summary vs detail, no contract invention. |
| `.trellis/tasks/archive/2026-05/05-20-continue-next-page-information-architecture-optimization/research/targets-nodes-list-control-audit.md` | Prior list-control audit pattern for safe frontend-only IA passes and preserving URL/API/runtime contracts. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-remaining-page-information-architecture/research/detail-special-pages-audit.md` | Earlier detail/specialized audit that called out SettingsPage tabs/channel manager vs linear spec mismatch and inline-style debt. |

### Current SettingsPage IA Structure and Pain Points

#### Current visible structure

- `SettingsPage` renders as a single `<form className="page-stack settings-page">`, so all tabs share one global submit (`web/src/pages/SettingsPage.tsx:473-588`).
- The page hero contains eyebrow `设置`, title `设置 / Settings`, a description, and pill tabs (`通用与外观`, `通知与告警`, `高级与策略`) (`web/src/pages/SettingsPage.tsx:475-493`).
- `通用与外观` tab contains `ThemeSettingsSection` and `FrequencyDefaultsSection` (`web/src/pages/SettingsPage.tsx:495-510`).
- `通知与告警` tab conditionally renders active Telegram/Feishu channel sections, then a `渠道管理 / 新增通知渠道` section, then `IncidentDefaultsSection` (`web/src/pages/SettingsPage.tsx:512-562`).
- `高级与策略` tab renders `OverrideRulesSection` and `RetentionPolicySection` (`web/src/pages/SettingsPage.tsx:564-579`).
- Save error/success and the single `保存设置` button are rendered after all tab panes, outside tab-specific sections (`web/src/pages/SettingsPage.tsx:581-588`).
- Notification channel add/configure is a `Modal`; `TelegramSettingsSection` and `FeishuSettingsSection` can render with `wrapper="none"` inside the modal (`web/src/pages/SettingsPage.tsx:590-687`, `web/src/pages/settings/TelegramSettingsSection.tsx:77-99`, `web/src/pages/settings/FeishuSettingsSection.tsx:47-70`).

#### Code/data structure

- Server response is converted into `SettingsFormState` in `buildFormState()`; numbers become strings for editable inputs, override arrays become formatted JSON strings, and `telegramBotToken` starts empty (`web/src/pages/SettingsPage.tsx:47-94`).
- Update payload is built in `buildUpdateInput()`; it validates integer/number fields, JSON arrays, Telegram token/chat pairing, runtime-managed requirements, Feishu values, override rules, and retention policy (`web/src/pages/SettingsPage.tsx:96-303`).
- Page state includes `loading`, `saving`, `error`, `saveError`, `saveSuccess`, `settings`, and `form` (`web/src/pages/SettingsPage.tsx:25-33`).
- Channel UI state is separate: `modalState`, `channelDraft`, `activeChannels`, and `expandedChannels` (`web/src/pages/SettingsPage.tsx:317-321`).
- Initial active channels are inferred from persisted Telegram token/runtime fields or Feishu form values, but only when the current `activeChannels` set is empty (`web/src/pages/SettingsPage.tsx:323-341`).

#### IA/spec mismatches and pain points

- Active component spec describes Settings as a linear ordered page: hero, `主题`, `Telegram`, `频率档位`, `全局默认`, `覆盖规则`, `保留策略`, then bottom save/error/success (`docs/design/v2-houfeng/component-spec.md:291-299`). Current code instead uses top-level tabs and a notification-channel object/add modal (`web/src/pages/SettingsPage.tsx:473-687`). Treat this as accepted current code unless this task intentionally updates only frontend IA around the tabbed model; do not infer backend/API changes from the old linear template.
- Global save feedback is structurally distant from tab-local edits because feedback and save button are after all panes (`web/src/pages/SettingsPage.tsx:581-588`). A limited cleanup can improve presentation/anchoring without changing global submit semantics.
- Notification channel sections can be absent until added because they depend on `activeChannels.has('telegram'|'feishu')` (`web/src/pages/SettingsPage.tsx:512-540`). This supports channel management but means notification settings are not always visible as normal settings sections.
- Inline style debt is substantial and explicitly known in specs: Settings uses inline layout/spacing/display in page panes and section components (`web/src/pages/SettingsPage.tsx:481-512`, `web/src/pages/settings/TelegramSettingsSection.tsx:23-63`, `web/src/pages/settings/FeishuSettingsSection.tsx:18-37`, `web/src/pages/settings/SectionIntro.tsx:7-8`). The styling spec only allows inline styles for dimensions/calculation and explicitly names SettingsPage inline spacing/layout as a known debt (`.trellis/spec/web/styling-guidelines.md:115-125`, `.trellis/spec/web/styling-guidelines.md:155-160`).
- Several existing settings CSS hooks already exist and can absorb cleanup safely: `.settings-page`, `.settings-actions`, `.settings-cluster`, `.settings-form-grid`, `.settings-form-grid--tight`, `.settings-fieldset`, `.settings-channel-option*`, `.override-rule-preview` (`web/src/styles/pages.css:1891-1944`, `web/src/styles/pages.css:2511-2516`, `web/src/styles/pages.css:2551-2560`, `web/src/styles/pages.css:3081-3107`, `web/src/styles/pages.css:6084-6112`).
- `OverrideRulesSection` defines `.override-rule-preview`, and CSS also defines `.override-rule-field`, but the component does not currently use `.override-rule-field` (`web/src/pages/settings/OverrideRulesSection.tsx:36-55`, `web/src/styles/pages.css:6084-6112`). This is a safe seam for replacing inline max-width/textarea styles without behavior changes.
- `FrequencyDefaultsSection` uses custom label/select markup rather than the `Input` atom because it renders `<select>` controls (`web/src/pages/settings/FrequencyDefaultsSection.tsx:26-41`). It is behaviorally simple and likely lower-risk than notification settings.

### Safe Improvements for This Task: UI Seams and Likely Files

Keep the task as **frontend-only IA/styling composition cleanup**. Do not change request/response types, endpoint usage, payload field names, validation semantics, notification delivery, or channel persistence semantics.

#### Safe seams in `SettingsPage.tsx`

1. **Replace tab-pane inline display wrappers with BEM classes**
   - Current seams: tab content wrappers use `style={{ display: activeTab === ... ? 'contents' : 'none' }}` (`web/src/pages/SettingsPage.tsx:495`, `web/src/pages/SettingsPage.tsx:512`, `web/src/pages/SettingsPage.tsx:564`).
   - Likely files: `web/src/pages/SettingsPage.tsx`, `web/src/styles/pages.css`.
   - Safe shape: add a settings tab-panel class/modifier or hidden attribute while preserving all mounted sections if tests rely on `display: none` content staying in the DOM. If switching to conditional render, verify no form state or tests depend on hidden tab content being queryable.

2. **Move hero-tab spacing into a class**
   - Current seam: wrapper around `Tabs` uses `style={{ marginTop: 'var(--space-4)' }}` (`web/src/pages/SettingsPage.tsx:481`).
   - Likely files: `web/src/pages/SettingsPage.tsx`, `web/src/styles/pages.css`.
   - Safe shape: `.settings-page__tabs` or similar under `.settings-page`.

3. **Make save feedback/action a clearer page-level footer without changing submit**
   - Current seam: save error/success panels and `.settings-actions` appear after tab panes (`web/src/pages/SettingsPage.tsx:581-588`).
   - Likely files: `web/src/pages/SettingsPage.tsx`, `web/src/styles/pages.css`, maybe `SettingsPage.test.tsx` if visible copy/query location changes.
   - Safe shape: wrap feedback + save in a named footer class; keep `<button type="submit">保存设置</button>` and existing success/error text.
   - Frozen: do not create per-tab save behavior; `handleSubmit` must continue to build and send the full settings payload.

4. **Clarify tab grouping copy, not behavior**
   - Current hero copy says the page maintains Telegram notifications, default frequencies, global rules, overrides, and retention (`web/src/pages/SettingsPage.tsx:478-480`).
   - Likely files: `web/src/pages/SettingsPage.tsx`, `SettingsPage.test.tsx` only if tested copy changes.
   - Safe shape: add a compact tab context/overview line or labels that make `通用与外观`, `通知与告警`, `高级与策略` map to current sections. Avoid adding new settings categories.

#### Safe seams in notification section components

1. **Replace inline layout on Telegram/Feishu section bodies and asides**
   - Current Telegram seams: body padding top, status summary max width, runtime field grid span, aside flex layout (`web/src/pages/settings/TelegramSettingsSection.tsx:23-63`, `web/src/pages/settings/TelegramSettingsSection.tsx:86-95`).
   - Current Feishu seams: body padding top, field grid span, aside flex layout (`web/src/pages/settings/FeishuSettingsSection.tsx:18-37`, `web/src/pages/settings/FeishuSettingsSection.tsx:57-66`).
   - Likely files: `TelegramSettingsSection.tsx`, `FeishuSettingsSection.tsx`, `pages.css`.
   - Safe shape: classes such as `settings-section-body`, `settings-section-status`, `settings-section-aside`, `settings-fieldset--wide`. Preserve labels, input names, and button names (`编辑`, `收起`) because tests query them.

2. **Keep existing channel modal draft semantics intact**
   - Current modal draft is isolated in `channelDraft`; closing resets it via `closeChannelModal()` (`web/src/pages/SettingsPage.tsx:404-407`, `web/src/pages/SettingsPage.tsx:590-687`).
   - Tests explicitly assert dismissed channel modal drafts are not persisted (`web/src/pages/SettingsPage.test.tsx:556-610`).
   - Safe shape: presentational class cleanup only; do not merge modal draft into main form until `confirmChannel()`.

3. **Keep channel collapsed/expanded semantics intact**
   - `TelegramSettingsSection` and `FeishuSettingsSection` render body only when `isExpanded` is true in detail wrapper mode (`web/src/pages/settings/TelegramSettingsSection.tsx:81-99`, `web/src/pages/settings/FeishuSettingsSection.tsx:51-70`).
   - Existing tests use `编辑` to reveal Telegram fields (`web/src/pages/SettingsPage.test.tsx:101-104`, `web/src/pages/SettingsPage.test.tsx:126-143`). Preserve this affordance or update tests intentionally.

#### Safe seams in settings subcomponents

1. **`SectionIntro` style to BEM class**
   - Current helper has inline font/color/line-height (`web/src/pages/settings/SectionIntro.tsx:7-8`).
   - Likely files: `SectionIntro.tsx`, `pages.css`.
   - Safe shape: render `<div className="settings-section-intro">` and style with tokens. Text content remains unchanged.

2. **`OverrideRulesSection` class-based textarea/preview layout**
   - Current textarea wrapper, max width, textarea min-height/padding/resize are inline (`web/src/pages/settings/OverrideRulesSection.tsx:37-45`).
   - CSS already has `.override-rule-preview` and unused `.override-rule-field` (`web/src/styles/pages.css:6084-6112`).
   - Likely files: `OverrideRulesSection.tsx`, `pages.css`.
   - Safe shape: use `.override-rule-field`, `.override-rule-field__textarea` or similar; keep valid JSON preview behavior exactly (`web/src/pages/SettingsPage.test.tsx:470-525`).

3. **`IncidentDefaultsSection` threshold grid class-only improvements**
   - Current fieldset grid span is inline for notification toggles (`web/src/pages/settings/IncidentDefaultsSection.tsx:106-122`).
   - Likely files: `IncidentDefaultsSection.tsx`, `pages.css`.
   - Safe shape: `settings-fieldset settings-fieldset--wide`; do not change labels or validation mapping because tests query `心跳间隔秒数` and expect exact error copy (`web/src/pages/SettingsPage.test.tsx:527-554`).

4. **`RetentionPolicySection` and `FrequencyDefaultsSection` should remain behaviorally unchanged**
   - Retention copy is asserted and must remain truthful (`web/src/pages/settings/RetentionPolicySection.tsx:29-55`, `web/src/pages/SettingsPage.test.tsx:160-166`).
   - Frequency copy is asserted and must remain truthful (`web/src/pages/settings/FrequencyDefaultsSection.tsx:49-74`, `web/src/pages/SettingsPage.test.tsx:119-124`).

### Frozen Contracts / Tests That Must Not Regress

#### API/data contracts

- `getSettings()` must remain `requestJSON<SettingsRecord>('/api/settings')` (`web/src/lib/api.ts:345-347`).
- `updateSettings(settings)` must remain a `PUT /api/settings` JSON request (`web/src/lib/api.ts:349-354`, tested in `web/src/pages/SettingsPage.test.tsx:331-339`).
- `SettingsRecord` and `SettingsUpdateInput` include only current frontend settings fields: `telegram`, `feishu`, `host_sample_frequency_tier`, `probe_frequency_defaults`, `incident_defaults`, `override_rules`, `retention_policy` (`web/src/lib/types.ts:537-545`, `web/src/lib/types.ts:1071-1079`). Do not add/change fields for IA cleanup.
- `SettingsTelegramInput` has optional `bot_token`, required `chat_id`, optional `runtime_managed`; unchanged token omission is a hard contract (`web/src/lib/types.ts:435-439`).

#### Telegram/Feishu secret and runtime contracts

- Telegram token input starts empty and persisted token is represented only by `token_present` + `token_masked_summary` (`web/src/pages/SettingsPage.tsx:47-52`, `web/src/lib/types.ts:417-423`).
- When a persisted token exists and no replacement token is entered, payload must omit `bot_token` and include only `chat_id` + `runtime_managed` (`web/src/pages/SettingsPage.tsx:145-150`; tested in `web/src/pages/SettingsPage.test.tsx:195-280`, `web/src/pages/SettingsPage.test.tsx:390-468`).
- When replacement token is entered, payload includes `bot_token` and preserves runtime toggle state (`web/src/pages/SettingsPage.tsx:227-232`; tested in `web/src/pages/SettingsPage.test.tsx:282-388`).
- Runtime managed toggle must remain checked when persisted settings explicitly disable Telegram delivery (`web/src/pages/SettingsPage.test.tsx:169-193`).
- Runtime status copy must preserve the three current states: not runtime-managed, runtime active, runtime-managed but disabled (`web/src/pages/settings/TelegramSettingsSection.tsx:63-72`).
- Feishu fields must remain `enabled` + `webhook_url` and must keep current enabled/copy behavior (`web/src/pages/settings/FeishuSettingsSection.tsx:19-42`).

#### Global save semantics

- One form submit sends the full settings payload, not a tab-local patch (`web/src/pages/SettingsPage.tsx:436-471`).
- Success text is currently asserted exactly: `设置已保存。保留策略会由中心后台执行；仍仅持久化保存的策略不会立即影响运行时。` (`web/src/pages/SettingsPage.tsx:461`, `web/src/pages/SettingsPage.test.tsx:225-229`, `web/src/pages/SettingsPage.test.tsx:324-328`, `web/src/pages/SettingsPage.test.tsx:418-422`). Avoid changing unless tests are intentionally updated.
- Numeric validation uses strict positive integer/number parsing; malformed integer text like `30abc` and `1.5` for integer fields must fail before fetch (`web/src/pages/SettingsPage.tsx:96-111`, `web/src/pages/SettingsPage.test.tsx:527-554`).
- Override rules must parse as JSON arrays before save (`web/src/pages/SettingsPage.tsx:113-123`).

#### Existing tests and query-sensitive UI labels

- Loading state: `正在加载设置…` (`web/src/pages/SettingsPage.test.tsx:114`).
- Main heading: `设置 / Settings` (`web/src/pages/SettingsPage.test.tsx:116`).
- Tabs used by tests: `通用与外观`, `通知与告警`, `高级与策略` (`web/src/pages/SettingsPage.test.tsx:97-104`, `web/src/pages/SettingsPage.test.tsx:147`, `web/src/pages/SettingsPage.test.tsx:218`, `web/src/pages/SettingsPage.test.tsx:313-317`, `web/src/pages/SettingsPage.test.tsx:537`).
- Buttons used by tests: `编辑`, `保存设置`, `+ 新增通知渠道`, `Telegram`, `关闭` (`web/src/pages/SettingsPage.test.tsx:101-104`, `web/src/pages/SettingsPage.test.tsx:223`, `web/src/pages/SettingsPage.test.tsx:591-601`).
- Form labels used by tests: `当前节点主机样本频率`, `Telegram Chat ID`, `新的 Telegram Bot Token`, `运行时接管`, `原始层保留天数`, `节点标签覆盖规则 JSON`, `目标类型覆盖规则 JSON`, `心跳间隔秒数` (`web/src/pages/SettingsPage.test.tsx:121`, `web/src/pages/SettingsPage.test.tsx:131`, `web/src/pages/SettingsPage.test.tsx:309-319`, `web/src/pages/SettingsPage.test.tsx:515-523`, `web/src/pages/SettingsPage.test.tsx:539-550`).
- JSON preview behavior is explicitly tested: three valid previews initially, preview disappears for invalid/empty textarea values (`web/src/pages/SettingsPage.test.tsx:470-525`).
- Dismissed channel modal draft must not persist to payload (`web/src/pages/SettingsPage.test.tsx:556-610`).

### Recommended Acceptance Criteria

1. **Frontend-only IA cleanup**
   - Changes are limited to `web/src/pages/SettingsPage.tsx`, existing `web/src/pages/settings/*` components, `web/src/styles/pages.css`, and matching `SettingsPage.test.tsx` updates if labels/copy/DOM structure intentionally change.
   - No backend, API client shape, `web/src/lib/types.ts`, or notification delivery logic changes.

2. **Current settings group model stays intact**
   - Existing tabs (`通用与外观`, `通知与告警`, `高级与策略`) remain unless this task explicitly decides to update tests and current behavior around always-mounted tab content.
   - Current sections remain available: Theme, Frequency Defaults, Telegram, Feishu, Channel Manager, Incident Defaults, Override Rules, Retention Policy.
   - Global save remains one page-level submit and sends the full payload.

3. **Security and notification behavior unchanged**
   - Existing Telegram masked token summary remains the only persisted-token display; no raw token appears outside the password input.
   - Saving unrelated settings with a persisted Telegram token still omits `telegram.bot_token`.
   - Runtime toggle behavior and runtime status copy remain truthful.
   - Feishu enabled/webhook values keep existing payload shape and display semantics.

4. **Style/IA debt reduced without new styling system**
   - Business layout/spacing inline styles in SettingsPage and settings subcomponents are replaced with BEM classes in `pages.css` where practical.
   - No new page-local CSS file, CSS-in-JS, Tailwind, chart library, or dependency.
   - New/changed CSS uses existing design tokens and BEM naming under Settings-related blocks.

5. **Tests cover frozen contracts**
   - Existing SettingsPage tests remain green or are updated only for intentional copy/structure changes.
   - If save footer/channel/card structure changes, keep/add tests for token omission, replacement token payload, runtime managed disabled state, dismissed modal draft, JSON preview, and numeric validation.

### Verification Commands

Recommended focused verification for this frontend-only task:

```bash
cd web && npx vitest run src/pages/SettingsPage.test.tsx
cd web && npm run lint
cd web && npm run build
```

Recommended full web verification before PR/commit:

```bash
make verify-web
```

If implementation is visibly user-facing, also do a local browser sanity pass per project guidance and report local-only evidence; do not add Playwright/Cypress dependencies and do not commit bulk screenshots.

### Deferred / Out-of-Scope Items

- Backend/API/data model changes for `/api/settings`.
- Any change to notification delivery behavior, notifier runtime wiring, Telegram/Feishu persistence semantics, or retention worker behavior.
- Per-tab save, partial PATCH semantics, autosave, optimistic save, or splitting global settings into independent resources.
- Changing Telegram/Feishu token masking, exposing raw persisted secrets, or storing/copying secrets outside the intended input field.
- Adding/removing supported notification channel types beyond current Telegram/Feishu UI.
- Changing incident threshold semantics, override rule schema, retention policy semantics, or frequency tier allowed values.
- Large SettingsPage architecture rewrite, new state management library, React Query/Zustand/Redux, or backend-backed channel CRUD model.
- Updating active public docs/specs directly from this research task; if the tabbed Settings IA becomes the accepted authority, use the proper spec-update workflow separately.
- Any Node/Target/VPS/Events/Dashboard IA changes; prior archived IA reports are context only.

### External References

No external search was used. This audit is based on in-repository code, specs, active design docs, and archived Trellis task research.

### Related Specs

- `.trellis/spec/web/component-conventions.md` — page/component boundaries, controlled components, API-client-only request flow, Modal/Drawer focus conventions, and SettingsPage as an oversized known gap.
- `.trellis/spec/web/styling-guidelines.md` — token/BEM styling, no page-local CSS, inline style restrictions, and explicit Settings inline-style debt.
- `.trellis/spec/web/state-and-data.md` — request helper usage, snake_case contract discipline, no direct page fetch, local state guidance.
- `.trellis/spec/web/quality-guidelines.md` — Vitest/jsdom page-test conventions and `make verify-web` command portal.
- `docs/design/v2-houfeng/component-spec.md` — SettingsPage linear DetailSection template; current code diverges through tabs/channel modal, so treat it as context unless the implementation task updates spec separately.

## Caveats / Not Found

- No external web references were needed for this repository-specific IA audit.
- Current code diverges from the active Settings component-spec template; this audit does not decide whether the spec or current tabbed UI should become authoritative, only identifies safe cleanup seams.
- The audit did not run tests or the app; findings are static code/spec inspection.
- `pages.css` contains additional settings-related classes after the inspected sections; implementation should search before adding duplicate selectors.
