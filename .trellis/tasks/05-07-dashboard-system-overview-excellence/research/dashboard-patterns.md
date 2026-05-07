# Dashboard patterns for Houfeng

## Comparable patterns

- Monitoring dashboards commonly start with a clear current state and then provide supporting trend panels or recent change signals. Grafana positions dashboards as collections of visualizations that help monitor and analyze data, but the useful pattern for Houfeng is not “more charts”; it is a concise state plus a small trend cue.
- Infrastructure/server management tools typically give users a resource overview and a direct path into hosts/nodes, services/targets, events, and configuration. The homepage should therefore act as a command surface, not just a passive summary.
- Operational dashboards generally separate “what needs attention now” from “where do I manage the system”. If those are separated into many equal sections, the page becomes noisy; if only the attention list remains, the page feels underpowered.

## Implications for Houfeng

- Keep one main status conclusion at the top. It should answer “am I okay?” immediately.
- Add a compact 24h trend signal near the top because users need to know whether the situation is getting worse or recovering.
- Keep the attention queue as the primary abnormal-state body, but restructure rows around object name and issue, not technical ID.
- Add compact management entries inside the same workbench so the homepage remains useful even when the user is not immediately handling the top incident.
- Do not restore full group lists or event lists. Those belong on dedicated pages with filters.

## Chosen approach

Use the existing `DashboardOverview` fields and frontend atoms to create:

- a stronger Fleet State status panel with inline metrics and a small trend link;
- a clearer abnormal queue row hierarchy;
- a compact system navigation block in the workbench;
- improved context item titles while preserving deep links and progressive disclosure.
