# Comparable systems research for Dashboard enrichment

Date: 2026-05-07

## Sources

* Grafana Dashboards docs: https://grafana.com/docs/grafana/latest/visualizations/dashboards/
* Zabbix Dashboard docs: https://www.zabbix.com/documentation/5.0/en/manual/web_interface/frontend_sections/monitoring/dashboard
* Netdata organization docs: https://learn.netdata.cloud/docs/netdata-agent/configuration/organize-systems-metrics-and-alerts
* Netdata getting started docs: https://learn.netdata.cloud/docs/getting-started
* Proxmox VE admin guide: https://pve.proxmox.com/pve-docs-7/pve-admin-guide.html
* Cockpit guide: https://cockpit-project.org/guide/latest/

## What similar systems do well

### Grafana

Grafana treats dashboards as arranged panels that create an at-a-glance view of related information. It also emphasizes links, variables, annotations, and grouping as dashboard controls.

What Houfeng should absorb:

* Keep dashboard content related to one operating question.
* Use links as navigation from overview to detail.
* Use grouping sparingly to keep overview readable.

What Houfeng should not copy now:

* A configurable panel grid. Houfeng is too early-stage and the user is asking for a useful default, not a dashboard builder.

### Zabbix

Zabbix Dashboard is designed to summarize important information. Its widget set includes problems, problem hosts, problems by severity, host availability, system information, and action log.

What Houfeng should absorb:

* Monitoring homepages need problem context, availability/context, and recent activity.
* Problem information is valuable when summarized by impact and severity.

What Houfeng should not copy now:

* A large widget catalog or many same-weight blocks. The previous Houfeng issue was exactly too many same-weight areas.

### Netdata

Netdata emphasizes organizing many systems through Spaces, Rooms, virtual nodes, labels, and infrastructure-wide dashboards. It also surfaces nodes, system overview, composite charts, and out-of-the-box alerts.

What Houfeng should absorb:

* Show how the monitored fleet is organized, but keep it compressed.
* Node and label/group context helps operators understand impact scope.
* Avoid making users navigate away just to learn whether the problem is isolated or broad.

What Houfeng should not copy now:

* Real-time charts and metric correlations. Houfeng Dashboard contract currently does not include CPU/memory/disk time-series facts.

### Proxmox VE

Proxmox positions the web UI as a clean overview of the whole cluster, with cluster health/resource summary and task history available from node panels.

What Houfeng should absorb:

* Server management homepages should communicate scope and management state, not only incidents.
* Recent task/activity context is useful, but should be compact.

What Houfeng should not copy now:

* Resource utilization graphs, because Houfeng does not yet have a global resource contract on Dashboard.

### Cockpit

Cockpit is an interactive admin interface for Linux machines with direct paths to services, logs, terminal, and system components. Its guide also makes system logs a first-class component with filtering.

What Houfeng should absorb:

* Management dashboards need paths to action surfaces, logs/events, and service/system views.
* Logs/events should be one click away and filterable, but the homepage does not need to become the log viewer.

## Converged Houfeng direction

Add one compact `运行上下文` strip inside the existing Dashboard workbench:

1. `影响范围`: summarizes affected groups or fleet scope using `group_summaries`.
2. `库存状态`: summarizes nodes/targets and setup/running management state.
3. `最近活动`: summarizes the latest event type and timestamp, linking to EventsPage.

This gives Houfeng the missing operational context without restoring the old noisy sections.
