#!/usr/bin/env python3
"""Run local browser sanity checks for Houfeng v2 UI work."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import sys
from dataclasses import dataclass
from typing import Iterable, Literal
from urllib.parse import parse_qs, urljoin, urlparse


VIEWPORT_RE = re.compile(r"^(?P<width>[1-9][0-9]*)x(?P<height>[1-9][0-9]*)$")
MOCK_API_PROFILE_CHOICES = ("none", "asset-workflows", "observability-support")
MockAPIProfile = Literal["none", "asset-workflows", "observability-support"]


@dataclass(frozen=True)
class Viewport:
    label: str
    width: int
    height: int


@dataclass(frozen=True)
class BrowserLogin:
    username: str
    password: str


def parse_viewport(value: str) -> Viewport:
    match = VIEWPORT_RE.fullmatch(value)
    if not match:
        raise argparse.ArgumentTypeError(
            f"viewport must look like 1440x1000, got {value!r}"
        )
    return Viewport(
        label=value,
        width=int(match.group("width")),
        height=int(match.group("height")),
    )


def normalize_route(route: str) -> str:
    if not route.startswith("/"):
        return f"/{route}"
    return route


def route_path(route: str) -> str:
    parsed = urlparse(normalize_route(route))
    return parsed.path or "/"


def target_url(base_url: str, route: str) -> str:
    base = base_url.rstrip("/") + "/"
    return urljoin(base, normalize_route(route).lstrip("/"))


def resolve_login_value(
    value: str | None,
    env_name: str | None,
    label: str,
) -> tuple[str | None, list[str]]:
    errors: list[str] = []
    if value and env_name:
        errors.append(f"{label}: use either a direct value or an env var, not both")
        return None, errors
    if env_name:
        env_value = os.environ.get(env_name)
        if not env_value:
            errors.append(f"{label}: environment variable {env_name!r} is not set or empty")
            return None, errors
        return env_value, errors
    if value:
        return value, errors
    return None, errors


def resolve_browser_login(args: argparse.Namespace) -> tuple[BrowserLogin | None, list[str]]:
    username, username_errors = resolve_login_value(
        args.login_username,
        args.login_username_env,
        "login username",
    )
    password, password_errors = resolve_login_value(
        args.login_password,
        args.login_password_env,
        "login password",
    )
    errors = username_errors + password_errors
    if errors:
        return None, errors

    if bool(username) != bool(password):
        return None, [
            "browser-sanity login requires both username and password; use "
            "--login-username or --login-username-env together with "
            "--login-password or --login-password-env"
        ]
    if username is None or password is None:
        return None, []
    return BrowserLogin(username=username, password=password), []


def iso_date(days_from_today: int) -> str:
    return (dt.date.today() + dt.timedelta(days=days_from_today)).isoformat()


def iso_timestamp(days_from_today: int, hour: int = 8) -> str:
    target = dt.datetime.combine(
        dt.date.today() + dt.timedelta(days=days_from_today),
        dt.time(hour=hour, minute=0, tzinfo=dt.timezone.utc),
    )
    return target.isoformat().replace("+00:00", "Z")


def asset_workflow_providers() -> list[dict[str, object]]:
    return [
        {
            "provider_id": "provider_hetzner",
            "name": "Hetzner",
            "website": "https://www.hetzner.com",
            "panel_url": "https://console.hetzner.cloud",
            "account_hint": "ops-main",
            "country": "DE",
            "note": "Primary EU compute account.",
            "rating": 5,
            "labels": ["core", "eu"],
            "created_at": iso_timestamp(-120),
            "updated_at": iso_timestamp(-2),
        },
        {
            "provider_id": "provider_vultr",
            "name": "Vultr",
            "website": "https://www.vultr.com",
            "panel_url": "https://my.vultr.com",
            "account_hint": "edge-lab",
            "country": "US",
            "note": "Edge and migration candidates.",
            "rating": 4,
            "labels": ["edge", "backup"],
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp(-3),
        },
        {
            "provider_id": "provider_netcup",
            "name": "Netcup",
            "website": "https://www.netcup.de",
            "panel_url": "https://www.servercontrolpanel.de",
            "account_hint": "legacy-eu",
            "country": "DE",
            "note": "Legacy low-cost instances under review.",
            "rating": 3,
            "labels": ["legacy"],
            "created_at": iso_timestamp(-300),
            "updated_at": iso_timestamp(-7),
        },
    ]


def asset_workflow_vps_assets() -> list[dict[str, object]]:
    return [
        {
            "vps_id": "vps_ams_core",
            "display_name": "ams-core-01",
            "provider_id": "provider_hetzner",
            "provider_name": "Hetzner",
            "product_name": "CPX31",
            "order_ref": "HZ-2026-001",
            "country": "NL",
            "region": "EU-West",
            "city": "Amsterdam",
            "datacenter": "AMS1",
            "ipv4": "192.0.2.10",
            "ipv6": "2001:db8:10::1",
            "ssh_host": "ams-core-01.example.test",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Debian 12",
            "virtualization": "kvm",
            "lifecycle_status": "active",
            "usage_status": "in_use",
            "renewal_decision": "unreviewed",
            "importance": "critical",
            "labels": ["prod", "web"],
            "note": "Primary asset workflow fixture with complete facts.",
            "active_monitoring_instance_link_count": 2,
            "running_monitoring_instance_count": 0,
            "running_target_count": 0,
            "created_at": iso_timestamp(-100),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
        },
        {
            "vps_id": "vps_sjc_edge",
            "display_name": "sjc-edge-02",
            "provider_id": "provider_vultr",
            "provider_name": "Vultr",
            "product_name": "High Frequency 2 vCPU",
            "order_ref": "VU-2026-EDGE",
            "country": "US",
            "region": "US-West",
            "city": "San Jose",
            "datacenter": "SJC",
            "ipv4": "198.51.100.24",
            "ipv6": "",
            "ssh_host": "sjc-edge-02.example.test",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Ubuntu 24.04",
            "virtualization": "kvm",
            "lifecycle_status": "to_migrate",
            "usage_status": "standby",
            "renewal_decision": "migrate",
            "importance": "normal",
            "labels": ["edge", "migration"],
            "note": "Migration candidate with active subscription evidence.",
            "active_monitoring_instance_link_count": 1,
            "running_monitoring_instance_count": 0,
            "running_target_count": 0,
            "created_at": iso_timestamp(-80),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
        },
        {
            "vps_id": "vps_tokyo_lab",
            "display_name": "tokyo-lab-unlinked",
            "provider_id": None,
            "provider_name": "",
            "product_name": "",
            "order_ref": "",
            "country": "",
            "region": "",
            "city": "",
            "datacenter": "",
            "ipv4": "",
            "ipv6": "",
            "ssh_host": "",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "",
            "virtualization": "",
            "lifecycle_status": "testing",
            "usage_status": "unknown",
            "renewal_decision": "unreviewed",
            "importance": "low",
            "labels": ["needs-facts"],
            "note": "Fixture row intentionally missing subscription, provider, location, and access facts.",
            "active_monitoring_instance_link_count": 0,
            "running_monitoring_instance_count": 0,
            "running_target_count": 0,
            "created_at": iso_timestamp(-20),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
        },
        {
            "vps_id": "vps_fra_legacy",
            "display_name": "fra-legacy-cancel",
            "provider_id": "provider_netcup",
            "provider_name": "Netcup",
            "product_name": "RS 1000",
            "order_ref": "NC-LEGACY",
            "country": "DE",
            "region": "EU-Central",
            "city": "Frankfurt",
            "datacenter": "FRA",
            "ipv4": "203.0.113.7",
            "ipv6": "",
            "ssh_host": "",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Debian 11",
            "virtualization": "kvm",
            "lifecycle_status": "to_cancel",
            "usage_status": "idle",
            "renewal_decision": "cancel",
            "importance": "low",
            "labels": ["legacy", "cost-review"],
            "note": "Cancel queue fixture with auto-renew cancelled.",
            "active_monitoring_instance_link_count": 1,
            "running_monitoring_instance_count": 1,
            "running_target_count": 1,
            "created_at": iso_timestamp(-240),
            "updated_at": iso_timestamp(-4),
            "archived_at": None,
        },
        {
            "vps_id": "vps_archive_old",
            "display_name": "archive-old-2019",
            "provider_id": "provider_netcup",
            "provider_name": "Netcup",
            "product_name": "Legacy VPS",
            "order_ref": "NC-2019",
            "country": "DE",
            "region": "EU-Central",
            "city": "Nuremberg",
            "datacenter": "NBG",
            "ipv4": "203.0.113.40",
            "ipv6": "",
            "ssh_host": "",
            "ssh_port": 22,
            "ssh_user": "root",
            "os_name": "Debian 10",
            "virtualization": "kvm",
            "lifecycle_status": "archived",
            "usage_status": "idle",
            "renewal_decision": "keep",
            "importance": "low",
            "labels": ["archived"],
            "note": "Archived fixture row for quick-view coverage.",
            "active_monitoring_instance_link_count": 0,
            "running_monitoring_instance_count": 0,
            "running_target_count": 0,
            "created_at": iso_timestamp(-900),
            "updated_at": iso_timestamp(-60),
            "archived_at": iso_timestamp(-30),
        },
    ]


def asset_workflow_subscriptions() -> list[dict[str, object]]:
    return [
        {
            "subscription_id": "sub_ams_core",
            "vps_id": "vps_ams_core",
            "price": 10.5,
            "currency": "USD",
            "billing_cycle": "monthly",
            "billing_months": 1,
            "monthly_price": 10.5,
            "started_at": iso_date(-100),
            "renew_at": iso_date(8),
            "auto_renew": True,
            "auto_renew_cancelled": False,
            "status": "active",
            "payment_method": "card-main",
            "note": "Renewal window fixture.",
            "created_at": iso_timestamp(-100),
            "updated_at": iso_timestamp(-1),
        },
        {
            "subscription_id": "sub_sjc_edge",
            "vps_id": "vps_sjc_edge",
            "price": 24.0,
            "currency": "USD",
            "billing_cycle": "quarterly",
            "billing_months": 3,
            "monthly_price": 8.0,
            "started_at": iso_date(-80),
            "renew_at": iso_date(21),
            "auto_renew": False,
            "auto_renew_cancelled": False,
            "status": "active",
            "payment_method": "paypal-edge",
            "note": "Migration candidate subscription.",
            "created_at": iso_timestamp(-80),
            "updated_at": iso_timestamp(-2),
        },
        {
            "subscription_id": "sub_fra_legacy",
            "vps_id": "vps_fra_legacy",
            "price": 5.0,
            "currency": "EUR",
            "billing_cycle": "monthly",
            "billing_months": 1,
            "monthly_price": 5.0,
            "started_at": iso_date(-240),
            "renew_at": iso_date(5),
            "auto_renew": True,
            "auto_renew_cancelled": True,
            "status": "active",
            "payment_method": "sepa",
            "note": "Cancel queue subscription with auto-renew cancelled.",
            "created_at": iso_timestamp(-240),
            "updated_at": iso_timestamp(-4),
        },
        {
            "subscription_id": "sub_archive_old",
            "vps_id": "vps_archive_old",
            "price": 3.0,
            "currency": "EUR",
            "billing_cycle": "monthly",
            "billing_months": 1,
            "monthly_price": 3.0,
            "started_at": iso_date(-900),
            "renew_at": iso_date(-20),
            "auto_renew": False,
            "auto_renew_cancelled": True,
            "status": "cancelled",
            "payment_method": "legacy-card",
            "note": "Archived subscription fixture.",
            "created_at": iso_timestamp(-900),
            "updated_at": iso_timestamp(-30),
        },
    ]


def lifecycle_context_for(
    vps_id: str,
    subscription_state: str,
    message: str,
) -> dict[str, object]:
    vps = next(row for row in asset_workflow_vps_assets() if row["vps_id"] == vps_id)
    return {
        "vps_id": vps["vps_id"],
        "display_name": vps["display_name"],
        "lifecycle_status": vps["lifecycle_status"],
        "renewal_decision": vps["renewal_decision"],
        "subscription_state": subscription_state,
        "message": message,
    }


def asset_workflow_monitoring_instance_contexts() -> list[dict[str, object]]:
    return [
        {
            "monitoring_instance_id": "mi_hkg_edge_01",
            "linked_vps_count": 1,
            "cancellation_attention": True,
            "summaries": [
                lifecycle_context_for(
                    "vps_fra_legacy",
                    "expired",
                    "关联 VPS 已待取消，监控实例仍在运行，需确认监控和退役动作。",
                )
            ],
        },
        {
            "monitoring_instance_id": "mi_ams_conflict_03",
            "linked_vps_count": 1,
            "cancellation_attention": False,
            "summaries": [
                lifecycle_context_for(
                    "vps_ams_core",
                    "active",
                    "关联 VPS 仍在用，订阅保持生效。",
                )
            ],
        },
    ]


def asset_workflow_target_contexts() -> list[dict[str, object]]:
    return [
        {
            "target_id": "target_api_core",
            "linked_vps_count": 1,
            "cancellation_attention": True,
            "summaries": [
                lifecycle_context_for(
                    "vps_fra_legacy",
                    "expired",
                    "服务挂载的 VPS 已待取消，Target 仍启用，需确认归档或迁移。",
                )
            ],
            "service_ids": ["svc_fra_api"],
            "domain_ids": ["dom_fra_api"],
        },
        {
            "target_id": "target_www_maint",
            "linked_vps_count": 1,
            "cancellation_attention": False,
            "summaries": [
                lifecycle_context_for(
                    "vps_ams_core",
                    "active",
                    "服务挂载的 VPS 仍在用。",
                )
            ],
            "service_ids": ["svc_ams_www"],
            "domain_ids": ["dom_ams_www"],
        },
    ]


def asset_workflow_monitoring_instances() -> list[dict[str, object]]:
    return [
        {
            "monitoring_instance_id": "mi_hkg_edge_01",
            "display_name": "fra legacy runtime",
            "group": "asset-fixture",
            "region": "EU-Central",
            "city": "Frankfurt",
            "provider": "Netcup",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "已绑定",
            "labels": ["legacy", "vps-linked"],
            "note": "Monitoring Instance intentionally still running for lifecycle workbench evidence.",
            "current_health_status": "关注",
            "last_heartbeat_at": iso_timestamp(0),
            "last_sync_at": iso_timestamp(0),
            "current_active_incident_count": 1,
            "current_primary_issue_summary": "legacy service still responds on cancelled host",
            "created_at": iso_timestamp(-120),
            "updated_at": iso_timestamp(0),
        },
        {
            "monitoring_instance_id": "mi_ams_conflict_03",
            "display_name": "ams-core-runtime",
            "group": "asset-fixture",
            "region": "EU-West",
            "city": "Amsterdam",
            "provider": "Hetzner",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "已绑定",
            "labels": ["prod", "vps-linked"],
            "note": "Healthy linked monitoring_instance fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp(0),
            "last_sync_at": iso_timestamp(0),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-100),
            "updated_at": iso_timestamp(0),
        },
    ]


def asset_workflow_targets() -> list[dict[str, object]]:
    return [
        {
            "target_id": "target_api_core",
            "name": "legacy-api.example.test",
            "target_type": "service",
            "host": "legacy-api.example.test",
            "base_port": 443,
            "execution_monitoring_instance_labels": ["legacy"],
            "run_status": "启用",
            "group": "asset-fixture",
            "labels": ["legacy", "api"],
            "note": "Target remains enabled until lifecycle action is confirmed.",
            "current_health_status": "关注",
            "current_active_incident_count": 1,
            "current_primary_issue_summary": "Host is linked to a VPS marked to_cancel.",
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp(-2),
        },
        {
            "target_id": "target_www_maint",
            "name": "www-core.example.test",
            "target_type": "service",
            "host": "www-core.example.test",
            "base_port": 443,
            "execution_monitoring_instance_labels": ["prod"],
            "run_status": "启用",
            "group": "asset-fixture",
            "labels": ["prod"],
            "note": "Healthy active VPS context fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-100),
            "updated_at": iso_timestamp(-1),
        },
    ]


def asset_workflow_monitoring_instance_sparklines() -> dict[str, object]:
    return {
        "monitoring_instances": {
            row["monitoring_instance_id"]: {
                "cpu_usage_pct": [24, 31, 42, 45, 36, 30],
                "mem_used_pct": [40, 43, 47, 50, 49, 46],
                "disk_used_pct": [55, 55, 56, 56, 57, 57],
            }
            for row in asset_workflow_monitoring_instances()
        }
    }


def asset_workflow_target_sparklines() -> dict[str, object]:
    return {
        "targets": {
            row["target_id"]: {
                "latency": [120, 140, 165, 190, 170, 150],
            }
            for row in asset_workflow_targets()
        }
    }


def asset_workflow_vps_monitoring_instance_links(vps_id: str) -> list[dict[str, object]]:
    if vps_id != "vps_fra_legacy":
        return []
    return [
        {
            "monitoring_instance_id": "mi_hkg_edge_01",
            "display_name": "fra legacy runtime",
            "group": "asset-fixture",
            "region": "EU-Central",
            "city": "Frankfurt",
            "provider": "Netcup",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "已绑定",
            "current_health_status": "关注",
            "last_heartbeat_at": iso_timestamp(0),
            "last_sync_at": iso_timestamp(0),
            "current_active_incident_count": 1,
            "current_primary_issue_summary": "legacy service still responds on cancelled host",
            "linked_at": iso_timestamp(-120),
            "note": "Monitoring Instance intentionally still running for lifecycle workbench evidence.",
        }
    ]


def asset_workflow_services(vps_id: str) -> list[dict[str, object]]:
    if vps_id != "vps_fra_legacy":
        return []
    return [
        {
            "service_id": "svc_fra_api",
            "vps_id": "vps_fra_legacy",
            "target_id": "target_api_core",
            "name": "Legacy API",
            "service_type": "api",
            "status": "active",
            "url": "https://legacy-api.example.test",
            "port": 443,
            "labels": ["legacy", "api"],
            "note": "Target remains enabled until lifecycle action is confirmed.",
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp(-2),
        }
    ]


def asset_workflow_domains(vps_id: str) -> list[dict[str, object]]:
    if vps_id != "vps_fra_legacy":
        return []
    return [
        {
            "domain_id": "dom_fra_api",
            "vps_id": "vps_fra_legacy",
            "service_id": "svc_fra_api",
            "target_id": "target_api_core",
            "domain_name": "legacy-api.example.test",
            "purpose": "legacy api",
            "status": "active",
            "registrar": "NameSilo",
            "expires_at": iso_date(25),
            "auto_renew": False,
            "https_enabled": True,
            "labels": ["legacy"],
            "note": "Domain should be reviewed with the retiring VPS.",
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp(-2),
        }
    ]


def asset_workflow_vps_detail(vps_id: str) -> dict[str, object] | None:
    vps = next((row for row in asset_workflow_vps_assets() if row["vps_id"] == vps_id), None)
    if vps is None:
        return None
    detail = dict(vps)
    detail["monitoring_instance_links"] = asset_workflow_vps_monitoring_instance_links(vps_id)
    return detail


def asset_workflow_vps_timeline(vps_id: str) -> dict[str, object] | None:
    if not any(row["vps_id"] == vps_id for row in asset_workflow_vps_assets()):
        return None
    return {
        "vps_id": vps_id,
        "renewal_decisions": [],
        "price_histories": [],
        "ip_histories": [],
        "spec_snapshots": [],
        "experience_logs": [],
    }


def asset_workflow_cancellation_preview(vps_id: str) -> dict[str, object] | None:
    detail = asset_workflow_vps_detail(vps_id)
    if detail is None:
        return None
    subscriptions = [
        {
            "record": row,
            "role": "active" if row["status"] == "active" else "inactive",
            "recommended_action": (
                "cancel_auto_renew_and_mark_cancelled"
                if row["status"] == "active"
                else "keep_inactive"
            ),
            "message": (
                "订阅仍处于 active，需要显式确认取消订阅自动续费并标记为 cancelled。"
                if row["status"] == "active"
                else "订阅已处于非活跃状态，仍需处理 VPS、Monitoring Instance 与实例状态。"
            ),
        }
        for row in asset_workflow_subscriptions()
        if row["vps_id"] == vps_id
    ]
    services = asset_workflow_services(vps_id)
    domains = asset_workflow_domains(vps_id)
    target_links = []
    if vps_id == "vps_fra_legacy":
        target_links.append(
            {
                "target_id": "target_api_core",
                "name": "legacy-api.example.test",
                "run_status": "启用",
                "service_ids": ["svc_fra_api"],
                "domain_ids": ["dom_fra_api"],
                "last_linked_at": iso_timestamp(-2),
            }
        )
    return {
        "vps": detail,
        "subscriptions": subscriptions,
        "monitoring_instance_links": detail["monitoring_instance_links"],
        "services": services,
        "domains": domains,
        "target_links": target_links,
        "recommended_steps": [
            {
                "object_type": "vps",
                "object_id": vps_id,
                "step_type": "vps_lifecycle",
                "from_state": f"{detail['lifecycle_status']}/{detail['renewal_decision']}",
                "to_state": "cancelled/cancel",
                "required": True,
                "message": "将 VPS 续费决策设为 cancel，并根据订阅到期情况设置生命周期。",
            }
        ],
        "warnings": [
            "仍有 1 个关联 Monitoring Instance 未标记不续费或已退役。",
            "仍有 1 个关联 Target/实例处于运行或维护状态。",
        ]
        if vps_id == "vps_fra_legacy"
        else [],
        "blockers": [],
    }


def asset_workflow_dashboard() -> dict[str, object]:
    return {
        "snapshot_generated_at": iso_timestamp(0),
        "total_monitoring_instance_count": 3,
        "total_target_count": 5,
        "abnormal_monitoring_instance_count": 1,
        "abnormal_target_count": 1,
        "severe_monitoring_instance_count": 0,
        "severe_target_count": 0,
        "maintenance_monitoring_instance_count": 0,
        "maintenance_target_count": 1,
        "pending_onboarding_monitoring_instance_count": 1,
        "paused_monitoring_instance_count": 0,
        "retired_monitoring_instance_count": 0,
        "paused_target_count": 1,
        "archived_target_count": 0,
        "recent_new_incident_count": 2,
        "recent_recovery_count": 1,
        "group_summaries": [
            {
                "group": "asset-fixture",
                "monitoring_instance_count": 3,
                "target_count": 5,
                "abnormal_monitoring_instance_count": 1,
                "abnormal_target_count": 1,
                "severe_monitoring_instance_count": 0,
                "severe_target_count": 0,
                "maintenance_monitoring_instance_count": 0,
                "maintenance_target_count": 1,
            }
        ],
        "notification_status": {
            "telegram_configured": True,
            "telegram_runtime_managed": True,
            "telegram_runtime_apply_active": True,
            "feishu_configured": False,
        },
        "asset_summary": {
            "renewal_due_30d_subscription_count": 3,
            "renewal_due_30d_vps_count": 3,
            "unreviewed_vps_count": 2,
            "to_cancel_vps_count": 1,
            "cancelled_vps_count": 0,
            "cancellation_attention_vps_count": 2,
            "running_cancelled_asset_count": 2,
            "to_migrate_vps_count": 1,
            "unlinked_vps_count": 3,
            "abnormal_linked_vps_count": 1,
            "cost_by_currency": [
                {"currency": "USD", "monthly_total": 18.5, "yearly_total": 222.0},
                {"currency": "EUR", "monthly_total": 8.0, "yearly_total": 96.0},
            ],
        },
        "abnormal_monitoring_instances": [],
        "abnormal_targets": [],
        "recent_events": [],
        "new_incident_trend_24h": [0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0],
        "recovery_trend_24h": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0],
    }


def observability_support_monitoring_instances() -> list[dict[str, object]]:
    return [
        {
            "monitoring_instance_id": "mi_hkg_edge_01",
            "display_name": "hkg-edge-01",
            "group": "asset-prod",
            "region": "APAC",
            "city": "Hong Kong",
            "provider": "Hetzner",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "已绑定",
            "labels": ["prod", "edge", "vps-linked"],
            "note": "Severe monitoring_instance fixture for UX-5 abnormal evidence.",
            "current_health_status": "严重",
            "last_heartbeat_at": iso_timestamp_hours_ago(1),
            "last_sync_at": iso_timestamp_hours_ago(1),
            "current_active_incident_count": 3,
            "current_primary_issue_summary": "CPU 持续高位且心跳延迟，需要先核对 VPS 负载。",
            "created_at": iso_timestamp(-80),
            "updated_at": iso_timestamp_hours_ago(1),
        },
        {
            "monitoring_instance_id": "mi_pending_sfo_02",
            "display_name": "sfo-pending-onboarding",
            "group": "asset-intake",
            "region": "US-West",
            "city": "San Francisco",
            "provider": "Vultr",
            "lifecycle_status": "待接入",
            "monitoring_status": "启用",
            "binding_status": "未绑定",
            "labels": ["onboarding", "needs-agent"],
            "note": "Pending onboarding fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": None,
            "last_sync_at": None,
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-6),
            "updated_at": iso_timestamp_hours_ago(6),
        },
        {
            "monitoring_instance_id": "mi_ams_conflict_03",
            "display_name": "ams-conflict-03",
            "group": "asset-prod",
            "region": "EU-West",
            "city": "Amsterdam",
            "provider": "Hetzner",
            "lifecycle_status": "在用",
            "monitoring_status": "启用",
            "binding_status": "指纹变更待确认",
            "labels": ["prod", "binding-review"],
            "note": "Fingerprint conflict fixture.",
            "current_health_status": "关注",
            "last_heartbeat_at": iso_timestamp_hours_ago(5),
            "last_sync_at": iso_timestamp_hours_ago(5),
            "current_active_incident_count": 1,
            "current_primary_issue_summary": "等待确认新的主机指纹。",
            "created_at": iso_timestamp(-60),
            "updated_at": iso_timestamp_hours_ago(5),
        },
        {
            "monitoring_instance_id": "mi_fra_maint_04",
            "display_name": "fra-maintenance-04",
            "group": "asset-ops",
            "region": "EU-Central",
            "city": "Frankfurt",
            "provider": "Netcup",
            "lifecycle_status": "在用",
            "monitoring_status": "维护中",
            "binding_status": "已绑定",
            "labels": ["maintenance", "db"],
            "note": "Maintenance window fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp_hours_ago(2),
            "last_sync_at": iso_timestamp_hours_ago(2),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-140),
            "updated_at": iso_timestamp_hours_ago(2),
        },
        {
            "monitoring_instance_id": "mi_sin_paused_05",
            "display_name": "sin-paused-05",
            "group": "asset-observe",
            "region": "APAC",
            "city": "Singapore",
            "provider": "Manual",
            "lifecycle_status": "观察中",
            "monitoring_status": "暂停",
            "binding_status": "已绑定",
            "labels": ["paused", "cost-review"],
            "note": "Paused monitoring fixture.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp_hours_ago(30),
            "last_sync_at": iso_timestamp_hours_ago(30),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-220),
            "updated_at": iso_timestamp_hours_ago(30),
        },
        {
            "monitoring_instance_id": "mi_old_retired_06",
            "display_name": "old-retired-06",
            "group": "archive",
            "region": "EU-Central",
            "city": "Nuremberg",
            "provider": "Netcup",
            "lifecycle_status": "已退役",
            "monitoring_status": "暂停",
            "binding_status": "已绑定",
            "labels": ["archived", "legacy"],
            "note": "Retired monitoring_instance fixture for inventory completeness.",
            "current_health_status": "正常",
            "last_heartbeat_at": iso_timestamp(-45),
            "last_sync_at": iso_timestamp(-45),
            "current_active_incident_count": 0,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-600),
            "updated_at": iso_timestamp(-45),
        },
    ]


def observability_support_targets() -> list[dict[str, object]]:
    return [
        {
            "target_id": "target_api_core",
            "name": "api-core.example.test",
            "target_type": "service",
            "host": "api-core.example.test",
            "base_port": 443,
            "execution_monitoring_instance_labels": ["prod", "edge"],
            "run_status": "启用",
            "group": "asset-prod",
            "labels": ["prod", "api", "vps-linked"],
            "note": "Abnormal API target fixture.",
            "current_health_status": "告警",
            "current_active_incident_count": 2,
            "last_success_at": iso_timestamp_hours_ago(7),
            "last_failure_at": iso_timestamp_hours_ago(1),
            "current_primary_issue_summary": "HTTP 5xx 持续出现，需结合 Monitoring Instance 与资产决策核对。",
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp_hours_ago(1),
        },
        {
            "target_id": "target_china_ref",
            "name": "china-reference-latency",
            "target_type": "china_reference",
            "host": "www.baidu.com",
            "base_port": 443,
            "execution_monitoring_instance_labels": ["cn-probe"],
            "run_status": "启用",
            "group": "network-reference",
            "labels": ["reference", "china", "notification"],
            "note": "Reference target with notification event coverage.",
            "current_health_status": "关注",
            "current_active_incident_count": 1,
            "last_success_at": iso_timestamp_hours_ago(4),
            "last_failure_at": iso_timestamp_hours_ago(2),
            "current_primary_issue_summary": "跨境参考延迟超过关注阈值。",
            "created_at": iso_timestamp(-70),
            "updated_at": iso_timestamp_hours_ago(2),
        },
        {
            "target_id": "target_www_maint",
            "name": "www-maintenance.example.test",
            "target_type": "service",
            "host": "www-maintenance.example.test",
            "base_port": 443,
            "execution_monitoring_instance_labels": ["prod"],
            "run_status": "维护中",
            "group": "asset-ops",
            "labels": ["maintenance", "web"],
            "note": "Maintenance target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": iso_timestamp_hours_ago(3),
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-120),
            "updated_at": iso_timestamp_hours_ago(3),
        },
        {
            "target_id": "target_docs_paused",
            "name": "docs-paused.example.test",
            "target_type": "service",
            "host": "docs-paused.example.test",
            "base_port": 443,
            "execution_monitoring_instance_labels": ["docs"],
            "run_status": "暂停",
            "group": "docs",
            "labels": ["paused", "docs"],
            "note": "Paused target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": iso_timestamp_hours_ago(48),
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-160),
            "updated_at": iso_timestamp_hours_ago(48),
        },
        {
            "target_id": "target_legacy_archived",
            "name": "legacy-archived.example.test",
            "target_type": "service",
            "host": "legacy-archived.example.test",
            "base_port": 80,
            "execution_monitoring_instance_labels": ["legacy"],
            "run_status": "已归档",
            "group": "archive",
            "labels": ["archived", "legacy"],
            "note": "Archived target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": iso_timestamp(-30),
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-500),
            "updated_at": iso_timestamp(-30),
        },
        {
            "target_id": "target_no_exec_labels",
            "name": "no-execution-label.example.test",
            "target_type": "service",
            "host": "no-execution-label.example.test",
            "base_port": 8443,
            "execution_monitoring_instance_labels": [],
            "run_status": "启用",
            "group": "asset-intake",
            "labels": ["needs-coverage"],
            "note": "Coverage-gap target fixture.",
            "current_health_status": "正常",
            "current_active_incident_count": 0,
            "last_success_at": None,
            "last_failure_at": None,
            "current_primary_issue_summary": "",
            "created_at": iso_timestamp(-12),
            "updated_at": iso_timestamp_hours_ago(12),
        },
    ]


def observability_support_events() -> list[dict[str, object]]:
    return [
        {
            "event_id": "event_monitoring_instance_severe_started",
            "incident_id": "inc_monitoring_instance_hkg_cpu",
            "incident_class": "monitoring_instance_resource_pressure",
            "object_type": "monitoring_instance",
            "object_id": "mi_hkg_edge_01",
            "event_type": "incident_started",
            "severity": "严重",
            "summary": "hkg-edge-01 CPU 与 load5 同时进入严重区间。",
            "created_at": iso_timestamp_hours_ago(1),
            "_labels": ["prod", "edge", "vps-linked"],
            "_notification_sent": True,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_api_escalated",
            "incident_id": "inc_target_api_5xx",
            "incident_class": "target_probe_failure",
            "object_type": "target",
            "object_id": "target_api_core",
            "event_type": "incident_escalated",
            "severity": "告警",
            "summary": "api-core.example.test HTTP 探测失败率升高。",
            "created_at": iso_timestamp_hours_ago(2),
            "_labels": ["prod", "api", "vps-linked"],
            "_notification_sent": True,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_china_notice",
            "incident_id": "inc_target_china_latency",
            "incident_class": "target_probe_failure",
            "object_type": "target",
            "object_id": "target_china_ref",
            "event_type": "incident_started",
            "severity": "关注",
            "summary": "中国参考入口延迟超过关注阈值，已发送通知样例。",
            "created_at": iso_timestamp_hours_ago(4),
            "_labels": ["reference", "china", "notification"],
            "_notification_sent": True,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_recovered",
            "incident_id": "inc_target_api_5xx",
            "incident_class": "target_probe_failure",
            "object_type": "target",
            "object_id": "target_api_core",
            "event_type": "incident_recovered",
            "severity": "正常",
            "summary": "api-core.example.test 探测恢复，保留用于 recovery filter。",
            "created_at": iso_timestamp_hours_ago(5),
            "_labels": ["prod", "api", "vps-linked"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_monitoring_instance_maintenance_entered",
            "incident_id": "runtime_monitoring_instance_fra_maint",
            "incident_class": "",
            "object_type": "monitoring_instance",
            "object_id": "mi_fra_maint_04",
            "event_type": "monitoring_instance_monitoring_maintenance_entered",
            "severity": "",
            "summary": "fra-maintenance-04 进入维护窗口。",
            "created_at": iso_timestamp_hours_ago(6),
            "_labels": ["maintenance", "db"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_maintenance_exited",
            "incident_id": "runtime_target_www_maint",
            "incident_class": "",
            "object_type": "target",
            "object_id": "target_www_maint",
            "event_type": "target_maintenance_exited",
            "severity": "",
            "summary": "www-maintenance.example.test 退出维护窗口。",
            "created_at": iso_timestamp_hours_ago(7),
            "_labels": ["maintenance", "web"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_backfilled_monitoring_instance",
            "incident_id": "inc_backfilled_monitoring_instance_disk",
            "incident_class": "monitoring_instance_disk_pressure",
            "object_type": "monitoring_instance",
            "object_id": "mi_hkg_edge_01",
            "event_type": "incident_started",
            "severity": "告警",
            "summary": "补传观测触发的磁盘压力事件，默认应被事件流排除。",
            "created_at": iso_timestamp_hours_ago(8),
            "_labels": ["prod", "edge", "backfilled"],
            "_notification_sent": False,
            "_is_backfilled": True,
        },
        {
            "event_id": "event_monitoring_instance_binding_confirmed",
            "incident_id": "binding_monitoring_instance_ams_conflict",
            "incident_class": "",
            "object_type": "monitoring_instance",
            "object_id": "mi_ams_conflict_03",
            "event_type": "monitoring_instance_binding_rebind_confirmed",
            "severity": "关注",
            "summary": "ams-conflict-03 新指纹确认重新绑定。",
            "created_at": iso_timestamp_hours_ago(10),
            "_labels": ["prod", "binding-review"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_paused",
            "incident_id": "runtime_target_docs_paused",
            "incident_class": "",
            "object_type": "target",
            "object_id": "target_docs_paused",
            "event_type": "target_paused",
            "severity": "",
            "summary": "docs-paused.example.test 已暂停探测。",
            "created_at": iso_timestamp_hours_ago(12),
            "_labels": ["paused", "docs"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
        {
            "event_id": "event_target_archived",
            "incident_id": "runtime_target_legacy_archived",
            "incident_class": "",
            "object_type": "target",
            "object_id": "target_legacy_archived",
            "event_type": "target_archived",
            "severity": "",
            "summary": "legacy-archived.example.test 已归档。",
            "created_at": iso_timestamp_hours_ago(36),
            "_labels": ["archived", "legacy"],
            "_notification_sent": False,
            "_is_backfilled": False,
        },
    ]


def observability_support_dashboard() -> dict[str, object]:
    monitoring = observability_support_monitoring_instances()
    targets = observability_support_targets()
    events = filter_observability_support_events({"limit": ["6"]})
    abnormal_monitoring_instances = [monitoring_instance for monitoring_instance in monitoring if monitoring_instance["current_health_status"] != "正常"]
    abnormal_targets = [target for target in targets if target["current_health_status"] != "正常"]

    group_names = sorted(
        {
            str(item.get("group") or "未分组")
            for item in [*monitoring, *targets]
        },
        key=lambda value: value or "未分组",
    )
    group_summaries: list[dict[str, object]] = []
    for group in group_names:
        group_monitoring_instances = [monitoring_instance for monitoring_instance in monitoring if str(monitoring_instance.get("group") or "未分组") == group]
        group_targets = [target for target in targets if str(target.get("group") or "未分组") == group]
        group_summaries.append(
            {
                "group": group,
                "monitoring_instance_count": len(group_monitoring_instances),
                "target_count": len(group_targets),
                "abnormal_monitoring_instance_count": sum(1 for monitoring_instance in group_monitoring_instances if monitoring_instance["current_health_status"] != "正常"),
                "abnormal_target_count": sum(1 for target in group_targets if target["current_health_status"] != "正常"),
                "severe_monitoring_instance_count": sum(1 for monitoring_instance in group_monitoring_instances if monitoring_instance["current_health_status"] == "严重"),
                "severe_target_count": sum(1 for target in group_targets if target["current_health_status"] == "严重"),
                "maintenance_monitoring_instance_count": sum(1 for monitoring_instance in group_monitoring_instances if monitoring_instance["monitoring_status"] == "维护中"),
                "maintenance_target_count": sum(1 for target in group_targets if target["run_status"] == "维护中"),
            }
        )

    return {
        "snapshot_generated_at": iso_timestamp_hours_ago(0),
        "total_monitoring_instance_count": len(monitoring),
        "total_target_count": len(targets),
        "abnormal_monitoring_instance_count": len(abnormal_monitoring_instances),
        "abnormal_target_count": len(abnormal_targets),
        "severe_monitoring_instance_count": sum(1 for monitoring_instance in monitoring if monitoring_instance["current_health_status"] == "严重"),
        "severe_target_count": sum(1 for target in targets if target["current_health_status"] == "严重"),
        "maintenance_monitoring_instance_count": sum(1 for monitoring_instance in monitoring if monitoring_instance["monitoring_status"] == "维护中"),
        "maintenance_target_count": sum(1 for target in targets if target["run_status"] == "维护中"),
        "pending_onboarding_monitoring_instance_count": sum(
            1
            for monitoring_instance in monitoring
            if monitoring_instance["lifecycle_status"] == "待接入"
            or monitoring_instance["binding_status"] in ("未绑定", "指纹变更待确认")
        ),
        "paused_monitoring_instance_count": sum(1 for monitoring_instance in monitoring if monitoring_instance["monitoring_status"] == "暂停"),
        "retired_monitoring_instance_count": sum(1 for monitoring_instance in monitoring if monitoring_instance["lifecycle_status"] == "已退役"),
        "paused_target_count": sum(1 for target in targets if target["run_status"] == "暂停"),
        "archived_target_count": sum(1 for target in targets if target["run_status"] == "已归档"),
        "recent_new_incident_count": sum(1 for event in events if event["event_type"] == "incident_started"),
        "recent_recovery_count": sum(1 for event in events if event["event_type"] == "incident_recovered"),
        "group_summaries": group_summaries,
        "notification_status": {
            "telegram_configured": True,
            "telegram_runtime_managed": False,
            "telegram_runtime_apply_active": False,
            "feishu_configured": False,
        },
        "asset_summary": {
            "renewal_due_30d_subscription_count": 0,
            "renewal_due_30d_vps_count": 0,
            "unreviewed_vps_count": 0,
            "to_cancel_vps_count": 0,
            "to_migrate_vps_count": 0,
            "unlinked_vps_count": 0,
            "abnormal_linked_vps_count": 0,
            "cost_by_currency": [],
        },
        "abnormal_monitoring_instances": [
            {
                "monitoring_instance_id": monitoring_instance["monitoring_instance_id"],
                "display_name": monitoring_instance["display_name"],
                "group": monitoring_instance["group"],
                "region": monitoring_instance["region"],
                "city": monitoring_instance["city"],
                "provider": monitoring_instance["provider"],
                "lifecycle_status": monitoring_instance["lifecycle_status"],
                "monitoring_status": monitoring_instance["monitoring_status"],
                "current_health_status": monitoring_instance["current_health_status"],
                "last_heartbeat_at": monitoring_instance["last_heartbeat_at"],
                "current_active_incident_count": monitoring_instance["current_active_incident_count"],
                "current_primary_issue_summary": monitoring_instance["current_primary_issue_summary"],
            }
            for monitoring_instance in abnormal_monitoring_instances[:4]
        ],
        "abnormal_targets": [
            {
                "target_id": target["target_id"],
                "name": target["name"],
                "target_type": target["target_type"],
                "host": target["host"],
                "base_port": target["base_port"],
                "run_status": target["run_status"],
                "group": target["group"],
                "current_health_status": target["current_health_status"],
                "last_success_at": target["last_success_at"],
                "last_failure_at": target["last_failure_at"],
                "current_active_incident_count": target["current_active_incident_count"],
                "current_primary_issue_summary": target["current_primary_issue_summary"],
            }
            for target in abnormal_targets[:4]
        ],
        "recent_events": events,
        "new_incident_trend_24h": [0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 1],
        "recovery_trend_24h": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0],
    }


def observability_monitoring_instance_sparklines(query: dict[str, list[str]]) -> dict[str, object]:
    metrics = [
        metric.strip()
        for value in query.get("metrics", ["cpu_usage_pct,mem_used_pct,disk_used_pct"])
        for metric in value.split(",")
        if metric.strip()
    ]
    defaults: dict[str, list[float | None]] = {
        "cpu_usage_pct": [42, 45, 48, 52, 57, 63, 72, 81, 88, 93, 96, 94],
        "mem_used_pct": [55, 56, 58, 61, 63, 67, 71, 76, 81, 85, 88, 91],
        "disk_used_pct": [61, 62, 63, 64, 66, 68, 70, 72, 73, 74, 76, 78],
    }
    stable: dict[str, list[float | None]] = {
        "cpu_usage_pct": [18, 19, 20, 21, 21, 22, 20, 19, 18, 20, 21, 19],
        "mem_used_pct": [34, 35, 35, 36, 35, 36, 37, 36, 36, 35, 35, 36],
        "disk_used_pct": [40, 40, 41, 41, 42, 42, 42, 43, 43, 43, 44, 44],
    }
    monitoring: dict[str, dict[str, list[float | None]]] = {}
    for monitoring_instance in observability_support_monitoring_instances():
        monitoring_instance_id = str(monitoring_instance["monitoring_instance_id"])
        source = defaults if monitoring_instance_id == "mi_hkg_edge_01" else stable
        if monitoring_instance["monitoring_status"] == "暂停":
            source = {key: [None, None, None, None] for key in defaults}
        monitoring[monitoring_instance_id] = {metric: source.get(metric, []) for metric in metrics}
    return {"monitoring_instances": monitoring}


def observability_support_monitoring_instance(monitoring_instance_id: str) -> dict[str, object] | None:
    return next(
        (
            row
            for row in observability_support_monitoring_instances()
            if row["monitoring_instance_id"] == monitoring_instance_id
        ),
        None,
    )


def observability_support_host_sample(
    monitoring_instance: dict[str, object],
    hours_ago: int,
    *,
    cpu: float,
    mem: float,
    disk: float,
) -> dict[str, object]:
    monitoring_instance_id = str(monitoring_instance["monitoring_instance_id"])
    observed_at = iso_timestamp_hours_ago(hours_ago)
    return {
        "monitoring_instance_id": monitoring_instance_id,
        "observed_at": observed_at,
        "received_at": observed_at,
        "agent_version": "visual-evidence",
        "fingerprint": f"fp-{monitoring_instance_id}",
        "cpu_usage_pct": cpu,
        "load_1": round(cpu / 18, 2),
        "load_5": round(cpu / 20, 2),
        "load_15": round(cpu / 24, 2),
        "mem_used_pct": mem,
        "mem_available_bytes": 2_147_483_648,
        "mem_total_bytes": 8_589_934_592,
        "swap_used_pct": 4,
        "disk_used_pct": disk,
        "disk_total_bytes": 85_899_345_920,
        "inode_used_pct": min(96, disk + 8),
        "net_in_bytes_per_sec": 2_400_000 + hours_ago * 1000,
        "net_out_bytes_per_sec": 1_700_000 + hours_ago * 900,
        "cpu_iowait_pct": 7 if cpu >= 90 else 2,
        "cpu_steal_pct": 1.2,
        "disk_read_bytes_per_sec": 6_200_000,
        "disk_write_bytes_per_sec": 3_800_000,
        "disk_busy_pct": 68 if disk >= 75 else 18,
        "uptime_seconds": 86400 * 18 + 3600,
        "maintenance_context": monitoring_instance["monitoring_status"] == "维护中",
        "is_backfilled": False,
        "sync_batch_id": f"sync_{monitoring_instance_id}_{hours_ago}",
        "containers": [
            {
                "id": "ctr-api",
                "name": "api",
                "image": "houfeng/api:visual",
                "status": "running",
                "cpu_pct": 12.4,
                "mem_pct": 23.8,
            }
        ]
        if hours_ago == 1
        else [],
    }


def observability_support_monitoring_instance_runtime_facts(monitoring_instance_id: str) -> dict[str, object] | None:
    monitoring_instance = observability_support_monitoring_instance(monitoring_instance_id)
    if monitoring_instance is None:
        return None
    severe = monitoring_instance_id == "mi_hkg_edge_01"
    recent = [
        observability_support_host_sample(
            monitoring_instance,
            hours_ago,
            cpu=92 - hours_ago if severe else 22 + (hours_ago % 3),
            mem=88 - hours_ago * 0.5 if severe else 36 + (hours_ago % 4),
            disk=78 - hours_ago * 0.2 if severe else 44,
        )
        for hours_ago in range(12, 0, -1)
    ]
    return {
        "monitoring_instance_id": monitoring_instance_id,
        "latest_host_sample": recent[-1] if monitoring_instance["monitoring_status"] != "暂停" else None,
        "recent_host_samples": recent if monitoring_instance["monitoring_status"] != "暂停" else [],
    }


def observability_support_monitoring_instance_onboarding(monitoring_instance_id: str) -> dict[str, object] | None:
    monitoring_instance = observability_support_monitoring_instance(monitoring_instance_id)
    if monitoring_instance is None:
        return None
    phase = "完成接入" if monitoring_instance["binding_status"] == "已绑定" else "等待确认主机指纹"
    state = dict(monitoring_instance)
    state.update(
        {
            "phase": phase,
            "has_host_sample": monitoring_instance["last_heartbeat_at"] is not None,
            "has_accepted_observation": monitoring_instance["current_active_incident_count"] > 0,
            "enrollment_token_issued_at": iso_timestamp_hours_ago(24),
            "current_binding_fingerprint_summary": f"fp-{monitoring_instance_id[:8]}",
        }
    )
    if monitoring_instance["binding_status"] == "指纹变更待确认":
        state["pending_binding"] = {
            "fingerprint_summary": f"pending-{monitoring_instance_id[:8]}",
            "first_seen_at": iso_timestamp_hours_ago(5),
            "last_seen_at": iso_timestamp_hours_ago(1),
            "attempt_count": 3,
        }
    return state


def observability_support_vps_for_monitoring_instance(monitoring_instance_id: str) -> list[dict[str, object]]:
    context = next(
        (
            row
            for row in asset_workflow_monitoring_instance_contexts()
            if row["monitoring_instance_id"] == monitoring_instance_id
        ),
        None,
    )
    if context is None:
        return []
    records: list[dict[str, object]] = []
    for summary in context.get("summaries", []):
        if not isinstance(summary, dict):
            continue
        vps_id = str(summary.get("vps_id") or "")
        vps = next((row for row in asset_workflow_vps_assets() if row["vps_id"] == vps_id), None)
        if vps is None:
            continue
        records.append(
            {
                "vps_id": vps["vps_id"],
                "display_name": vps["display_name"],
                "provider_id": vps.get("provider_id"),
                "provider_name": vps.get("provider_name") or "",
                "country": vps.get("country") or "",
                "region": vps.get("region") or "",
                "city": vps.get("city") or "",
                "lifecycle_status": vps.get("lifecycle_status") or "",
                "usage_status": vps.get("usage_status") or "",
                "renewal_decision": vps.get("renewal_decision") or "",
                "importance": vps.get("importance") or "normal",
                "labels": vps.get("labels") or [],
                "archived_at": vps.get("archived_at"),
                "linked_at": iso_timestamp(-20),
                "note": str(summary.get("message") or "visual evidence linked VPS"),
            }
        )
    return records


def filter_observability_support_incidents(query: dict[str, list[str]]) -> list[dict[str, object]]:
    object_type = first_query_value(query, "object_type")
    object_id = first_query_value(query, "object_id")
    rows = [
        {
            "incident_id": "inc_monitoring_instance_hkg_cpu",
            "incident_class": "monitoring_instance_resource_pressure",
            "object_type": "monitoring_instance",
            "object_id": "mi_hkg_edge_01",
            "severity": "严重",
            "started_at": iso_timestamp_hours_ago(3),
            "last_evaluated_at": iso_timestamp_hours_ago(1),
            "source_summary": "CPU 与 load5 持续高位，建议先核对 VPS 工作负载。",
        },
        {
            "incident_id": "inc_monitoring_instance_hkg_disk",
            "incident_class": "monitoring_instance_disk_pressure",
            "object_type": "monitoring_instance",
            "object_id": "mi_hkg_edge_01",
            "severity": "告警",
            "started_at": iso_timestamp_hours_ago(6),
            "last_evaluated_at": iso_timestamp_hours_ago(1),
            "source_summary": "磁盘使用率接近告警阈值。",
        },
    ]
    if object_type:
        rows = [row for row in rows if row["object_type"] == object_type]
    if object_id:
        rows = [row for row in rows if row["object_id"] == object_id]
    return rows


def observability_target_sparklines() -> dict[str, object]:
    return {
        "targets": {
            "target_api_core": {"latency": [120, 130, 180, 240, 300, 520, 860, 1100, 980, 760, 640, 720]},
            "target_china_ref": {"latency": [180, 210, 260, 310, 360, 390, 420, 450, 410, 380, 430, 470]},
            "target_www_maint": {"latency": [42, 41, 40, None, None, 39, 38, 37]},
            "target_docs_paused": {"latency": [None, None, None, None]},
            "target_legacy_archived": {"latency": []},
            "target_no_exec_labels": {"latency": [22, 24, 25, 23, 21, 24]},
        }
    }


def query_bool(query: dict[str, list[str]], key: str) -> bool:
    value = first_query_value(query, key)
    if value is None:
        return False
    return value.lower() in {"1", "true", "t", "yes", "y", "on"}


def parse_query_datetime(value: str | None) -> dt.datetime | None:
    if not value:
        return None
    normalized = value.replace("Z", "+00:00")
    try:
        parsed = dt.datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=dt.timezone.utc)
    return parsed.astimezone(dt.timezone.utc)


def event_datetime(event: dict[str, object]) -> dt.datetime | None:
    value = event.get("created_at")
    return parse_query_datetime(str(value)) if value else None


def public_event(event: dict[str, object]) -> dict[str, object]:
    return {key: value for key, value in event.items() if not key.startswith("_")}


def filter_observability_support_events(
    query: dict[str, list[str]],
) -> list[dict[str, object]]:
    rows = observability_support_events()
    include_backfilled = query_bool(query, "include_backfilled")
    if not include_backfilled:
        rows = [row for row in rows if not row.get("_is_backfilled")]

    for key in ("object_type", "object_id", "severity", "event_type"):
        value = first_query_value(query, key)
        if value:
            rows = [row for row in rows if str(row.get(key) or "") == value]

    created_from = parse_query_datetime(first_query_value(query, "created_from"))
    if created_from is not None:
        rows = [
            row for row in rows
            if (event_time := event_datetime(row)) is not None and event_time >= created_from
        ]
    created_to = parse_query_datetime(first_query_value(query, "created_to"))
    if created_to is not None:
        rows = [
            row for row in rows
            if (event_time := event_datetime(row)) is not None and event_time <= created_to
        ]

    label = first_query_value(query, "label")
    if label:
        rows = [row for row in rows if label in row.get("_labels", [])]
    if query_bool(query, "notification_only"):
        rows = [row for row in rows if row.get("_notification_sent")]
    if query_bool(query, "recovery_only"):
        rows = [row for row in rows if row.get("event_type") == "incident_recovered"]
    if query_bool(query, "maintenance_only"):
        maintenance_types = {
            "monitoring_instance_monitoring_maintenance_entered",
            "monitoring_instance_monitoring_maintenance_exited",
            "target_maintenance_entered",
            "target_maintenance_exited",
        }
        rows = [row for row in rows if row.get("event_type") in maintenance_types]

    rows.sort(key=lambda row: str(row.get("created_at") or ""), reverse=True)
    limit_value = first_query_value(query, "limit")
    try:
        limit = int(limit_value) if limit_value else 50
    except ValueError:
        limit = 50
    if limit <= 0:
        limit = 50
    return [public_event(row) for row in rows[:limit]]


def iso_timestamp_hours_ago(hours: int) -> str:
    target = dt.datetime.now(dt.timezone.utc) - dt.timedelta(hours=hours)
    return target.replace(microsecond=0).isoformat().replace("+00:00", "Z")


def first_query_value(query: dict[str, list[str]], key: str) -> str | None:
    values = query.get(key)
    if not values:
        return None
    value = values[0].strip()
    return value or None


def filter_asset_workflow_vps(query: dict[str, list[str]]) -> list[dict[str, object]]:
    rows = asset_workflow_vps_assets()
    for key in ("provider_id", "lifecycle_status", "usage_status", "renewal_decision"):
        value = first_query_value(query, key)
        if value:
            rows = [row for row in rows if str(row.get(key) or "") == value]
    return rows


def filter_asset_workflow_subscriptions(
    query: dict[str, list[str]],
) -> list[dict[str, object]]:
    rows = asset_workflow_subscriptions()
    vps_id = first_query_value(query, "vps_id")
    if vps_id:
        rows = [row for row in rows if row["vps_id"] == vps_id]

    status = first_query_value(query, "status")
    if status:
        rows = [row for row in rows if row["status"] == status]

    renew_within_days = first_query_value(query, "renew_within_days")
    if renew_within_days:
        try:
            window_days = int(renew_within_days)
        except ValueError:
            window_days = None
        if window_days is not None:
            today = dt.date.today()
            rows = [
                row
                for row in rows
                if isinstance(row.get("renew_at"), str)
                and (
                    dt.date.fromisoformat(str(row["renew_at"])) - today
                ).days
                <= window_days
            ]

    if first_query_value(query, "sort") == "renew_at":
        rows.sort(key=lambda row: str(row.get("renew_at") or "9999-12-31"))
        if first_query_value(query, "order") == "desc":
            rows.reverse()
    return rows


def fulfill_json(route: object, status: int, body: object) -> None:
    route.fulfill(
        status=status,
        content_type="application/json; charset=utf-8",
        body=json.dumps(body, ensure_ascii=False),
    )


def fulfill_asset_workflow_api(route: object) -> None:
    request = route.request
    parsed = urlparse(request.url)
    path = parsed.path
    query = parse_qs(parsed.query)
    method = request.method.upper()

    if method == "GET" and path == "/api/auth/me":
        fulfill_json(
            route,
            200,
            {
                "user_id": "user_visual_evidence",
                "username": "visual-evidence",
                "role": "admin",
                "display_name": "Visual Evidence",
            },
        )
        return

    if method == "GET" and path == "/api/dashboard":
        fulfill_json(route, 200, asset_workflow_dashboard())
        return

    if method == "GET" and path == "/api/providers":
        fulfill_json(route, 200, asset_workflow_providers())
        return

    if method == "GET" and path == "/api/vps":
        fulfill_json(route, 200, filter_asset_workflow_vps(query))
        return

    if method == "GET" and path.startswith("/api/vps/"):
        parts = [part for part in path.split("/") if part]
        if len(parts) >= 3:
            vps_id = parts[2]
            if len(parts) == 3:
                detail = asset_workflow_vps_detail(vps_id)
                if detail is not None:
                    fulfill_json(route, 200, detail)
                    return
            if len(parts) == 4 and parts[3] == "timeline":
                timeline = asset_workflow_vps_timeline(vps_id)
                if timeline is not None:
                    fulfill_json(route, 200, timeline)
                    return
            if len(parts) == 4 and parts[3] == "services":
                fulfill_json(route, 200, asset_workflow_services(vps_id))
                return
            if len(parts) == 4 and parts[3] == "domains":
                fulfill_json(route, 200, asset_workflow_domains(vps_id))
                return
            if len(parts) == 4 and parts[3] == "cancellation-preview":
                preview = asset_workflow_cancellation_preview(vps_id)
                if preview is not None:
                    fulfill_json(route, 200, preview)
                    return

    if method == "GET" and path == "/api/subscriptions":
        fulfill_json(route, 200, filter_asset_workflow_subscriptions(query))
        return

    if method == "GET" and path == "/api/asset-context/monitoring-instances":
        fulfill_json(route, 200, asset_workflow_monitoring_instance_contexts())
        return

    if method == "GET" and path == "/api/asset-context/targets":
        fulfill_json(route, 200, asset_workflow_target_contexts())
        return

    if method == "GET" and path == "/api/monitoring-instances":
        fulfill_json(route, 200, asset_workflow_monitoring_instances())
        return

    if method == "GET" and path == "/api/monitoring-instances/sparklines":
        fulfill_json(route, 200, asset_workflow_monitoring_instance_sparklines())
        return

    if method == "GET" and path == "/api/targets":
        fulfill_json(route, 200, asset_workflow_targets())
        return

    if method == "GET" and path == "/api/targets/sparklines":
        fulfill_json(route, 200, asset_workflow_target_sparklines())
        return

    fulfill_json(
        route,
        404,
        {
            "error": "mock asset workflow API has no fixture for this request",
            "method": method,
            "path": path,
        },
    )


def fulfill_observability_support_api(route: object) -> None:
    request = route.request
    parsed = urlparse(request.url)
    path = parsed.path
    query = parse_qs(parsed.query)
    method = request.method.upper()

    if method == "GET" and path == "/api/auth/me":
        fulfill_json(
            route,
            200,
            {
                "user_id": "user_observability_visual_evidence",
                "username": "observability-evidence",
                "role": "admin",
                "display_name": "Observability Evidence",
            },
        )
        return

    if method == "GET" and path == "/api/dashboard":
        fulfill_json(route, 200, observability_support_dashboard())
        return

    if method == "GET" and path == "/api/monitoring-instances":
        fulfill_json(route, 200, observability_support_monitoring_instances())
        return

    if method == "GET" and path.startswith("/api/monitoring-instances/"):
        parts = [part for part in path.split("/") if part]
        if len(parts) >= 3:
            monitoring_instance_id = parts[2]
            if len(parts) == 3:
                monitoring_instance = observability_support_monitoring_instance(monitoring_instance_id)
                if monitoring_instance is not None:
                    fulfill_json(route, 200, monitoring_instance)
                    return
            if len(parts) == 4 and parts[3] == "runtime-facts":
                facts = observability_support_monitoring_instance_runtime_facts(monitoring_instance_id)
                if facts is not None:
                    fulfill_json(route, 200, facts)
                    return
            if len(parts) == 4 and parts[3] == "onboarding":
                onboarding = observability_support_monitoring_instance_onboarding(monitoring_instance_id)
                if onboarding is not None:
                    fulfill_json(route, 200, onboarding)
                    return
            if len(parts) == 4 and parts[3] == "vps":
                fulfill_json(route, 200, observability_support_vps_for_monitoring_instance(monitoring_instance_id))
                return

    if method == "GET" and path == "/api/monitoring-instances/sparklines":
        fulfill_json(route, 200, observability_monitoring_instance_sparklines(query))
        return

    if method == "GET" and path == "/api/targets":
        fulfill_json(route, 200, observability_support_targets())
        return

    if method == "GET" and path == "/api/targets/sparklines":
        fulfill_json(route, 200, observability_target_sparklines())
        return

    if method == "GET" and path == "/api/asset-context/monitoring-instances":
        fulfill_json(route, 200, asset_workflow_monitoring_instance_contexts())
        return

    if method == "GET" and path == "/api/asset-context/targets":
        fulfill_json(route, 200, asset_workflow_target_contexts())
        return

    if method == "GET" and path == "/api/events":
        fulfill_json(route, 200, {"items": filter_observability_support_events(query)})
        return

    if method == "GET" and path == "/api/incidents":
        fulfill_json(route, 200, filter_observability_support_incidents(query))
        return

    fulfill_json(
        route,
        404,
        {
            "error": "mock observability support API has no fixture for this request",
            "method": method,
            "path": path,
        },
    )


def install_mock_api_routes(page: object, profile: MockAPIProfile) -> None:
    if profile == "none":
        return
    if profile == "asset-workflows":
        page.route("**/api/**", fulfill_asset_workflow_api)
        return
    if profile == "observability-support":
        page.route("**/api/**", fulfill_observability_support_api)
        return
    raise ValueError(f"unsupported mock API profile: {profile}")


def login_browser_context(
    page: object,
    base_url: str,
    login: BrowserLogin,
    timeout_ms: int,
) -> None:
    response = page.request.post(
        target_url(base_url, "/api/auth/login"),
        data={"username": login.username, "password": login.password},
        timeout=timeout_ms,
    )
    if response.status < 200 or response.status >= 300:
        body = response.text().strip()
        if len(body) > 240:
            body = body[:240] + "..."
        detail = f": {body}" if body else ""
        raise RuntimeError(f"POST /api/auth/login returned {response.status}{detail}")


def run_browser_sanity(
    base_url: str,
    routes: Iterable[str],
    viewports: Iterable[Viewport],
    timeout_ms: int,
    mock_api: MockAPIProfile,
    login: BrowserLogin | None,
) -> int:
    try:
        from playwright.sync_api import Error as PlaywrightError
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import sync_playwright
    except ModuleNotFoundError:
        print(
            "browser-sanity requires a locally installed Python Playwright package. "
            "The repository intentionally does not depend on it; install/use local "
            "browser tooling or record browser sanity as blocked.",
            file=sys.stderr,
        )
        return 2

    failures: list[str] = []
    with sync_playwright() as playwright:
        try:
            browser = playwright.chromium.launch()
        except PlaywrightError as exc:
            print(
                "browser-sanity could not launch Chromium through local Playwright. "
                "Install the browser runtime locally or record browser sanity as blocked.\n"
                f"{exc}",
                file=sys.stderr,
            )
            return 2

        try:
            for route in routes:
                for viewport in viewports:
                    url = target_url(base_url, route)
                    context = browser.new_context(
                        viewport={"width": viewport.width, "height": viewport.height}
                    )
                    page = context.new_page()
                    install_mock_api_routes(page, mock_api)
                    pre_navigation_failures: list[str] = []
                    if login is not None:
                        try:
                            login_browser_context(page, base_url, login, timeout_ms)
                        except (PlaywrightError, RuntimeError) as exc:
                            pre_navigation_failures.append(f"login failed: {exc}")

                    try:
                        page.goto(url, wait_until="networkidle", timeout=timeout_ms)
                    except PlaywrightTimeoutError:
                        # Long-polling or failed API calls can prevent networkidle.
                        page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
                    try:
                        page.wait_for_function(
                            "() => document.body && document.body.innerText.trim().length > 20",
                            timeout=timeout_ms,
                        )
                    except PlaywrightTimeoutError:
                        pass

                    result = page.evaluate(
                        """
                        () => {
                          const doc = document.documentElement;
                          const body = document.body;
                          const viewportWidth = window.innerWidth;
                          const viewportHeight = window.innerHeight;
                          const bodyText = (body.innerText || '').trim().replace(/\\s+/g, ' ');
                          const pagePanels = document.querySelectorAll('.page-panel, .hero-panel, main, [role="main"]').length;

                          function visible(el) {
                            const style = window.getComputedStyle(el);
                            const rect = el.getBoundingClientRect();
                            return style.display !== 'none'
                              && style.visibility !== 'hidden'
                              && Number(style.opacity || '1') > 0
                              && rect.width > 0
                              && rect.height > 0;
                          }

                          function hasVisibleText(el) {
                            const text = (el.innerText || el.textContent || '').trim();
                            return text.length > 0 && visible(el);
                          }

                          const elements = Array.from(document.querySelectorAll('body *'));
                          const leafTextElements = elements.filter((el) => {
                            if (!hasVisibleText(el)) return false;
                            return !Array.from(el.children).some((child) => hasVisibleText(child));
                          });

                          const overflowingText = leafTextElements
                            .filter((el) => el.scrollWidth > el.clientWidth + 2 || el.scrollHeight > el.clientHeight + 2)
                            .slice(0, 8)
                            .map((el) => {
                              const rect = el.getBoundingClientRect();
                              return {
                                tag: el.tagName.toLowerCase(),
                                className: String(el.className || ''),
                                text: (el.innerText || el.textContent || '').trim().replace(/\\s+/g, ' ').slice(0, 80),
                                clientWidth: el.clientWidth,
                                scrollWidth: el.scrollWidth,
                                clientHeight: el.clientHeight,
                                scrollHeight: el.scrollHeight,
                                x: Math.round(rect.x),
                                y: Math.round(rect.y),
                              };
                            });

                          return {
                            currentUrl: window.location.href,
                            title: document.title,
                            bodyText,
                            bodyTextLength: bodyText.length,
                            bodyTextSample: bodyText.slice(0, 100),
                            docScrollWidth: doc.scrollWidth,
                            bodyScrollWidth: body.scrollWidth,
                            viewportWidth,
                            viewportHeight,
                            pagePanels,
                            overflowingText,
                          };
                        }
                        """
                    )
                    page.close()
                    context.close()

                    route_failures = list(pre_navigation_failures)
                    if result["bodyTextLength"] < 20:
                        route_failures.append("blank-or-nearly-blank body text")
                    for marker in [
                        "mock asset workflow API has no fixture for this request",
                        "mock observability support API has no fixture for this request",
                        "页面不可用",
                        "列表不可用",
                        "详情不可用",
                        "VPS 库存不可用",
                        "取消/退役预览失败",
                    ]:
                        if marker in result["bodyText"]:
                            route_failures.append(f"visible error marker {marker!r}")
                    if result["docScrollWidth"] > viewport.width + 2:
                        route_failures.append(
                            f"document horizontal overflow {result['docScrollWidth']} > {viewport.width}"
                        )
                    if result["bodyScrollWidth"] > viewport.width + 2:
                        route_failures.append(
                            f"body horizontal overflow {result['bodyScrollWidth']} > {viewport.width}"
                        )
                    if mock_api != "none" or login is not None:
                        current_path = urlparse(result["currentUrl"]).path or "/"
                        expected_path = route_path(route)
                        if current_path != expected_path:
                            route_failures.append(
                                f"unexpected final path {current_path!r}; expected {expected_path!r}"
                            )
                    status = "PASS" if not route_failures else "FAIL"
                    warning_text = (
                        f" warnings={len(result['overflowingText'])}"
                        if result["overflowingText"]
                        else ""
                    )
                    mock_text = f" mock={mock_api}" if mock_api != "none" else ""
                    auth_text = " auth=session-login" if login is not None else ""
                    print(
                        f"{status} {normalize_route(route)} {viewport.label} "
                        f"text={result['bodyTextLength']} "
                        f"doc={result['docScrollWidth']} body={result['bodyScrollWidth']} "
                        f"panels={result['pagePanels']}{warning_text}{mock_text}{auth_text} "
                        f"url={result['currentUrl']}"
                    )
                    if result["overflowingText"]:
                        for item in result["overflowingText"]:
                            print(
                                "  warning overflow-risk "
                                f"{item['tag']}.{item['className']} "
                                f"{item['clientWidth']}x{item['clientHeight']} -> "
                                f"{item['scrollWidth']}x{item['scrollHeight']} "
                                f"at {item['x']},{item['y']}: {item['text']!r}"
                            )
                    if route_failures:
                        failures.append(
                            f"{normalize_route(route)} {viewport.label}: "
                            + "; ".join(route_failures)
                        )
        finally:
            browser.close()

    if failures:
        print("\nBrowser sanity failed:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)
        return 1
    return 0

def command_browser_sanity(args: argparse.Namespace) -> int:
    login, login_errors = resolve_browser_login(args)
    if login is not None and args.mock_api != "none":
        login_errors.append("browser-sanity real login cannot be combined with --mock-api")
    if login_errors:
        print("Browser sanity configuration error:", file=sys.stderr)
        for error in login_errors:
            print(f"- {error}", file=sys.stderr)
        return 2

    return run_browser_sanity(
        base_url=args.base_url,
        routes=args.route,
        viewports=args.viewport,
        timeout_ms=args.timeout_ms,
        mock_api=args.mock_api,
        login=login,
    )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Run Houfeng v2 local browser sanity checks."
    )
    subcommands = parser.add_subparsers(dest="command", required=True)

    sanity = subcommands.add_parser(
        "browser-sanity",
        help="Run a local-only browser sanity probe against a running preview URL.",
    )
    sanity.add_argument(
        "--base-url",
        required=True,
        help="Running preview URL, for example http://127.0.0.1:5178/.",
    )
    sanity.add_argument(
        "--route",
        action="append",
        required=True,
        help="Route to check. Repeat for multiple routes, for example --route /monitoring.",
    )
    sanity.add_argument(
        "--viewport",
        action="append",
        type=parse_viewport,
        default=[],
        help="Viewport to check, e.g. 1440x1000. Repeat for multiple viewports. Defaults to 1440x1000 and 390x900.",
    )
    sanity.add_argument(
        "--timeout-ms",
        type=int,
        default=10_000,
        help="Navigation and body-text wait timeout in milliseconds.",
    )
    sanity.add_argument(
        "--mock-api",
        choices=MOCK_API_PROFILE_CHOICES,
        default="none",
        help=(
            "Optional local-only API fixture profile. Use asset-workflows for "
            "protected Asset Ledger routes or observability-support for Monitoring, "
            "Targets, and Events without a running center."
        ),
    )
    sanity.add_argument(
        "--login-username",
        help="Username for an optional real center /api/auth/login session.",
    )
    sanity.add_argument(
        "--login-username-env",
        metavar="ENV_VAR",
        help="Environment variable containing the login username.",
    )
    sanity.add_argument(
        "--login-password",
        help=(
            "Password for an optional real center /api/auth/login session. "
            "Prefer --login-password-env to avoid exposing the value in process args."
        ),
    )
    sanity.add_argument(
        "--login-password-env",
        metavar="ENV_VAR",
        help="Environment variable containing the login password.",
    )
    sanity.set_defaults(func=command_browser_sanity)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if getattr(args, "command", None) == "browser-sanity" and not args.viewport:
        args.viewport = [parse_viewport("1440x1000"), parse_viewport("390x900")]
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
