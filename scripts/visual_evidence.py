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
ASSET_WORKFLOW_BASE_CURRENCY = "CNY"


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


def month_bucket(months_from_current: int) -> str:
    current = dt.date.today().replace(day=1)
    year = current.year + (current.month - 1 + months_from_current) // 12
    month = (current.month - 1 + months_from_current) % 12 + 1
    return f"{year:04d}-{month:02d}"


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
            "billing_period_unit": "month",
            "billing_period_length": 1,
            "monthly_price": 10.5,
            "started_at": iso_date(-100),
            "renew_at": iso_date(8),
            "auto_renew": True,
            "auto_renew_cancelled": False,
            "renewal_mode": "auto",
            "status": "active",
            "payment_method": "card-main",
            "display_name": "ams-core-01 月付",
            "cost_category": "compute",
            "labels": ["prod", "web"],
            "trial_ends_at": None,
            "ends_at": None,
            "note": "Renewal window fixture.",
            "monthly_price_base": 76.65,
            "yearly_price_base": 919.8,
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "exchange_rate": 7.3,
            "exchange_rate_date": iso_date(0),
            "exchange_rate_stale": False,
            "budget_status": "over",
            "next_reminder_at": iso_timestamp(1),
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
            "billing_period_unit": "month",
            "billing_period_length": 3,
            "monthly_price": 8.0,
            "started_at": iso_date(-80),
            "renew_at": iso_date(21),
            "auto_renew": False,
            "auto_renew_cancelled": False,
            "renewal_mode": "manual",
            "status": "active",
            "payment_method": "paypal-edge",
            "display_name": "sjc-edge 迁移候选",
            "cost_category": "edge",
            "labels": ["edge", "migration"],
            "trial_ends_at": None,
            "ends_at": iso_date(45),
            "note": "Migration candidate subscription.",
            "monthly_price_base": 58.4,
            "yearly_price_base": 700.8,
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "exchange_rate": 7.3,
            "exchange_rate_date": iso_date(-3),
            "exchange_rate_stale": True,
            "budget_status": "warning",
            "next_reminder_at": iso_timestamp(7),
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
            "billing_period_unit": "month",
            "billing_period_length": 1,
            "monthly_price": 5.0,
            "started_at": iso_date(-240),
            "renew_at": iso_date(5),
            "auto_renew": True,
            "auto_renew_cancelled": True,
            "renewal_mode": "auto_cancelled",
            "status": "active",
            "payment_method": "sepa",
            "display_name": "fra legacy 待取消",
            "cost_category": "legacy",
            "labels": ["legacy", "cost-review"],
            "trial_ends_at": None,
            "ends_at": None,
            "note": "Cancel queue subscription with auto-renew cancelled.",
            "monthly_price_base": 39.5,
            "yearly_price_base": 474.0,
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "exchange_rate": 7.9,
            "exchange_rate_date": iso_date(0),
            "exchange_rate_stale": False,
            "budget_status": "ok",
            "next_reminder_at": iso_timestamp(4),
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
            "billing_period_unit": "month",
            "billing_period_length": 1,
            "monthly_price": 3.0,
            "started_at": iso_date(-900),
            "renew_at": iso_date(-20),
            "auto_renew": False,
            "auto_renew_cancelled": True,
            "renewal_mode": "auto_cancelled",
            "status": "cancelled",
            "payment_method": "legacy-card",
            "display_name": "archive old cancelled",
            "cost_category": "archive",
            "labels": ["archived"],
            "trial_ends_at": None,
            "ends_at": iso_date(-20),
            "note": "Archived subscription fixture.",
            "monthly_price_base": 23.7,
            "yearly_price_base": 284.4,
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "exchange_rate": 7.9,
            "exchange_rate_date": iso_date(-30),
            "exchange_rate_stale": True,
            "budget_status": "disabled",
            "next_reminder_at": None,
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


def asset_decision_source_availability() -> dict[str, bool]:
    return {
        "subscriptions": True,
        "services": True,
        "domains": True,
        "monitoring": True,
        "targets": True,
    }


def asset_decision_current_facts_from_member(member: dict[str, object]) -> dict[str, object]:
    vps = member.get("vps") if isinstance(member.get("vps"), dict) else {}
    return {
        "found": bool(vps),
        "lifecycle_status": vps.get("lifecycle_status") if isinstance(vps, dict) else "",
        "usage_status": vps.get("usage_status") if isinstance(vps, dict) else "",
        "renewal_decision": vps.get("renewal_decision") if isinstance(vps, dict) else "",
        "active_subscription_count": member.get("active_subscription_count", 0),
        "service_count": member.get("service_count", 0),
        "domain_count": member.get("domain_count", 0),
        "target_count": member.get("target_count", 0),
        "running_target_count": member.get("running_target_count", 0),
        "monitoring_link_count": member.get("monitoring_link_count", 0),
        "running_monitoring_count": member.get("running_monitoring_count", 0),
        "abnormal_monitoring_count": member.get("abnormal_monitoring_count", 0),
        "active_incident_count": member.get("active_incident_count", 0),
        "source_availability": member.get("source_availability", asset_decision_source_availability()),
    }


def asset_decision_member_readback(
    member: dict[str, object],
    decided_action: str,
    followup_status: str,
) -> dict[str, object]:
    issues: list[dict[str, object]] = []
    status = "open"
    summary = "等待跟进执行"
    if followup_status == "blocked":
        status = "blocked"
        summary = "人工跟进被标记为阻塞"
        issues.append(asset_decision_chip("followup_blocked", "跟进阻塞", "critical"))
    elif decided_action in {"open_cancellation_workbench", "cancel"}:
        issues.extend(
            [
                asset_decision_chip("active_subscription", "仍有订阅", "alert"),
                asset_decision_chip("running_target", "Target 仍运行", "alert"),
                asset_decision_chip("running_monitoring", "监控仍运行", "alert"),
            ]
        )
        status = "drift"
        summary = "取消/退役判断尚未闭环"
    elif decided_action == "migrate":
        status = "open" if followup_status != "done" else "drift"
        summary = "迁移链路仍在推进"
        if followup_status == "done":
            issues.append(asset_decision_chip("old_workload_running", "旧承载未清理", "alert"))
            summary = "跟进完成但旧承载仍存在"
    elif decided_action == "keep":
        status = "aligned"
        summary = "当前事实支持保留判断"
    elif decided_action == "complete_evidence":
        status = "needs_evidence"
        summary = "仍需补齐资产证据"
        issues.append(asset_decision_chip("missing_evidence", "资料缺口", "alert"))
    return {
        "status": status,
        "summary": summary,
        "issues": issues,
        "current_facts": asset_decision_current_facts_from_member(member),
    }


def asset_decision_record_readback(members: list[dict[str, object]]) -> dict[str, object]:
    counts = {
        "open_count": 0,
        "aligned_count": 0,
        "drift_count": 0,
        "blocked_count": 0,
        "needs_evidence_count": 0,
    }
    for member in members:
        readback = member.get("execution_readback")
        if not isinstance(readback, dict):
            continue
        status = str(readback.get("status") or "open")
        if status == "aligned":
            counts["aligned_count"] += 1
        elif status == "drift":
            counts["drift_count"] += 1
        elif status == "blocked":
            counts["blocked_count"] += 1
        elif status == "needs_evidence":
            counts["needs_evidence_count"] += 1
        elif status == "open":
            counts["open_count"] += 1
    if counts["drift_count"] > 0:
        status = "drift"
        summary = f"{counts['drift_count']} 台 VPS 与执行目标存在漂移"
    elif counts["blocked_count"] > 0:
        status = "blocked"
        summary = f"{counts['blocked_count']} 台 VPS 跟进阻塞"
    elif counts["needs_evidence_count"] > 0:
        status = "needs_evidence"
        summary = f"{counts['needs_evidence_count']} 台 VPS 仍缺证据"
    elif counts["open_count"] > 0:
        status = "open"
        summary = f"{counts['open_count']} 台 VPS 仍待推进"
    else:
        status = "aligned"
        summary = "当前事实与组合决策一致"
    return {"status": status, "summary": summary, **counts}


def asset_decision_member_execution_plan(member: dict[str, object]) -> dict[str, object]:
    action = str(member.get("decided_action") or member.get("suggested_action") or "review")
    readback = member.get("execution_readback")
    if not isinstance(readback, dict):
        readback = {"status": "open", "issues": []}
    issues = readback.get("issues")
    if not isinstance(issues, list):
        issues = []
    missing_fact = any(isinstance(issue, dict) and issue.get("kind") == "current_fact_missing" for issue in issues)
    subscription_gap = any(
        isinstance(issue, dict)
        and issue.get("kind") == "evidence_gap"
        and ("订阅" in str(issue.get("label") or "") or "subscription" in str(issue.get("details") or "").lower())
        for issue in issues
    )
    if missing_fact or action == "review":
        lane = "review"
        step_kind = "review_record"
        step_label = "留在记录中复核"
    elif action in {"cancel", "open_cancellation_workbench"}:
        lane = "cancel_retire"
        step_kind = "open_cancellation_workbench"
        step_label = "打开取消/退役工作台"
    elif action == "migrate":
        lane = "migration"
        step_kind = "open_vps_detail"
        step_label = "打开 VPS 详情推进迁移"
    elif action in {"keep", "observe"}:
        lane = "keep_observe"
        step_kind = "open_vps_detail"
        step_label = "打开 VPS 详情核对判断"
    elif action == "complete_evidence":
        lane = "evidence"
        step_kind = "open_subscription_context" if subscription_gap else "open_vps_detail"
        step_label = "核对订阅上下文" if subscription_gap else "打开 VPS 详情补证据"
    else:
        lane = "review"
        step_kind = "review_record"
        step_label = "留在记录中复核"

    status = str(readback.get("status") or "open")
    tone_by_status = {
        "drift": "critical",
        "blocked": "critical",
        "needs_evidence": "alert",
        "open": "notice",
        "aligned": "normal",
        "inactive": "neutral",
    }
    summary_by_status = {
        "drift": "当前事实与判断不一致，需要复核闭环",
        "blocked": "成员跟进阻塞，需要解除阻塞或调整路径",
        "needs_evidence": "证据仍未补齐，先补上下文再确认判断",
        "open": "当前事实仍待处理或复核",
        "aligned": "当前事实已对齐，待确认跟进状态",
        "inactive": "记录已失效，不需要推进",
    }
    followup_status = str(member.get("followup_status") or "todo")
    actionable = status in {"drift", "blocked", "needs_evidence", "open"} or (status == "aligned" and followup_status not in {"done", "skipped"})
    return {
        "lane": lane,
        "step_kind": step_kind,
        "tone": tone_by_status.get(status, "neutral"),
        "summary": summary_by_status.get(status, "需要复核执行路径"),
        "step_label": step_label,
        "issue_count": len(issues),
        "blocked": status == "blocked" or followup_status == "blocked",
        "actionable": actionable,
    }


def asset_decision_record_execution_plan(members: list[dict[str, object]], readback: dict[str, object]) -> dict[str, object]:
    lane_order = ["cancel_retire", "migration", "keep_observe", "evidence", "review"]
    lane_counts: list[dict[str, object]] = []
    for lane in lane_order:
        count = sum(1 for member in members if isinstance(member.get("execution_plan"), dict) and member["execution_plan"].get("lane") == lane)
        if count:
            lane_counts.append({"lane": lane, "count": count})
    actionable_count = sum(1 for member in members if isinstance(member.get("execution_plan"), dict) and member["execution_plan"].get("actionable"))
    blocked_count = sum(1 for member in members if isinstance(member.get("execution_plan"), dict) and member["execution_plan"].get("blocked"))
    if int(readback.get("drift_count") or 0) > 0:
        summary = f"{readback['drift_count']} 台 VPS 事实漂移，优先复核闭环"
    elif blocked_count > 0:
        summary = f"{blocked_count} 台 VPS 跟进阻塞，需要解除阻塞"
    elif int(readback.get("needs_evidence_count") or 0) > 0:
        summary = f"{readback['needs_evidence_count']} 台 VPS 需要补齐证据"
    elif actionable_count > 0:
        summary = f"{actionable_count} 台 VPS 仍有执行步骤"
    else:
        summary = "执行计划已对齐"
    return {
        "summary": summary,
        "lane_counts": lane_counts,
        "actionable_count": actionable_count,
        "blocked_count": blocked_count,
    }


def asset_decision_chip(
    kind: str,
    label: str,
    tone: str,
    details: str | None = None,
) -> dict[str, object]:
    chip: dict[str, object] = {"kind": kind, "label": label, "tone": tone}
    if details:
        chip["details"] = details
    return chip


def asset_decision_evidence_assessment(
    *,
    confidence_score: int,
    pressure_score: int,
    readiness_score: int,
    quality_tier: str,
    decision_bias: str,
    support_signal_count: int,
    risk_signal_count: int,
    gap_signal_count: int,
    summary: str,
) -> dict[str, object]:
    return {
        "confidence_score": confidence_score,
        "pressure_score": pressure_score,
        "readiness_score": readiness_score,
        "quality_tier": quality_tier,
        "decision_bias": decision_bias,
        "support_signal_count": support_signal_count,
        "risk_signal_count": risk_signal_count,
        "gap_signal_count": gap_signal_count,
        "summary": summary,
    }


def asset_decision_member_assessment(
    suggested_action: str,
    evidence_chips: list[dict[str, object]],
) -> dict[str, object]:
    chip_kinds = {str(chip.get("kind") or "") for chip in evidence_chips}
    gap_count = sum(
        1
        for kind in chip_kinds
        if kind
        in {
            "missing_subscription",
            "missing_monitoring",
            "missing_provider",
            "missing_location",
            "missing_access",
            "no_service_context",
            "subscription_unavailable",
        }
    )
    risk_count = sum(
        1
        for kind in chip_kinds
        if kind
        in {
            "renewal_due",
            "idle_paid",
            "budget_risk",
            "abnormal_monitoring",
            "cancellation_linkage",
            "exchange_rate_stale",
        }
    )
    support_count = 2 + int("carries_service" in chip_kinds)
    confidence = max(18, min(92, 78 + support_count * 3 - gap_count * 14))
    pressure = max(8, min(96, risk_count * 22 + (30 if suggested_action in {"open_cancellation_workbench", "cancel"} else 0)))
    readiness = max(15, min(88, confidence + min(pressure // 4, 12) - gap_count * 10))
    if gap_count >= 3:
        tier = "blocked"
        bias = "complete_evidence"
        summary = f"证据阻塞：{gap_count} 项缺口，先补齐资料"
    elif gap_count > 0:
        tier = "weak"
        bias = "complete_evidence"
        summary = f"证据偏弱：{gap_count} 项缺口，先补证据"
    elif suggested_action in {"open_cancellation_workbench", "cancel"}:
        tier = "usable"
        bias = "retire"
        summary = "压力较高：优先复核取消联动"
    elif suggested_action == "migrate":
        tier = "usable"
        bias = "migrate"
        summary = "证据可用：偏向迁移观察"
    elif suggested_action == "keep" and confidence >= 80:
        tier = "strong"
        bias = "keep"
        summary = "证据完整：可保存组合判断"
    else:
        tier = "usable"
        bias = "review"
        summary = "证据可用：复核后决策"
    return asset_decision_evidence_assessment(
        confidence_score=confidence,
        pressure_score=pressure,
        readiness_score=readiness,
        quality_tier=tier,
        decision_bias=bias,
        support_signal_count=support_count,
        risk_signal_count=risk_count,
        gap_signal_count=gap_count,
        summary=summary,
    )


def asset_decision_group_assessment(
    members: list[dict[str, object]],
    group_type: str,
) -> dict[str, object]:
    assessments = [member["evidence_assessment"] for member in members]
    if not assessments:
        return asset_decision_evidence_assessment(
            confidence_score=0,
            pressure_score=0,
            readiness_score=0,
            quality_tier="weak",
            decision_bias="review",
            support_signal_count=0,
            risk_signal_count=0,
            gap_signal_count=0,
            summary="暂无成员证据",
        )
    count = len(assessments)
    confidence = round(sum(int(item["confidence_score"]) for item in assessments) / count)
    pressure = round(
        (
            sum(int(item["pressure_score"]) for item in assessments) / count
            + max(int(item["pressure_score"]) for item in assessments)
        )
        / 2
    )
    readiness = round(sum(int(item["readiness_score"]) for item in assessments) / count)
    gap_count = sum(int(item["gap_signal_count"]) for item in assessments)
    risk_count = sum(int(item["risk_signal_count"]) for item in assessments)
    support_count = sum(int(item["support_signal_count"]) for item in assessments)
    if group_type == "evidence_gap" or gap_count >= 3:
        tier = "blocked"
        bias = "complete_evidence"
        summary = f"证据阻塞：{gap_count} 项缺口，先补齐资料"
    elif group_type == "cancellation_attention":
        tier = "usable"
        bias = "retire"
        summary = "压力较高：优先复核取消联动"
    elif confidence >= 78 and readiness >= 70 and gap_count == 0:
        tier = "strong"
        bias = "keep"
        summary = "证据完整：可保存组合判断"
    elif pressure >= 68:
        tier = "usable"
        bias = "review"
        summary = f"压力较高：{risk_count} 项风险，优先复核"
    else:
        tier = "usable"
        bias = "review"
        summary = "证据可用：复核后决策"
    return asset_decision_evidence_assessment(
        confidence_score=max(0, min(100, confidence)),
        pressure_score=max(0, min(100, pressure)),
        readiness_score=max(0, min(100, readiness)),
        quality_tier=tier,
        decision_bias=bias,
        support_signal_count=support_count,
        risk_signal_count=risk_count,
        gap_signal_count=gap_count,
        summary=summary,
    )


def asset_decision_primary_subscription(vps_id: str) -> dict[str, object] | None:
    rows = [
        row
        for row in asset_workflow_subscriptions()
        if row["vps_id"] == vps_id and row["status"] == "active"
    ]
    rows.sort(key=lambda row: str(row.get("renew_at") or "9999-12-31"))
    return dict(rows[0]) if rows else None


def asset_decision_days_until(value: object) -> int | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        return (dt.date.fromisoformat(value) - dt.date.today()).days
    except ValueError:
        return None


def asset_decision_member(
    vps_id: str,
    *,
    suggested_role: str,
    suggested_action: str,
    evidence_chips: list[dict[str, object]],
    cancellation_attention_reason: str | None = None,
) -> dict[str, object]:
    vps = next(row for row in asset_workflow_vps_assets() if row["vps_id"] == vps_id)
    subscriptions = [row for row in asset_workflow_subscriptions() if row["vps_id"] == vps_id]
    primary_subscription = asset_decision_primary_subscription(vps_id)
    service_count = len(asset_workflow_services(vps_id))
    domain_count = len(asset_workflow_domains(vps_id))
    target_count = 1 if vps_id == "vps_fra_legacy" else 0
    monitoring_link_count = int(vps.get("active_monitoring_instance_link_count") or 0)
    abnormal_monitoring_count = 1 if vps_id == "vps_fra_legacy" else 0
    active_incident_count = 1 if vps_id == "vps_fra_legacy" else 0
    assessment = asset_decision_member_assessment(suggested_action, evidence_chips)
    return {
        "vps": dict(vps),
        "primary_subscription": primary_subscription,
        "subscription_count": len(subscriptions),
        "active_subscription_count": sum(1 for row in subscriptions if row["status"] == "active"),
        "inactive_subscription_count": sum(1 for row in subscriptions if row["status"] != "active"),
        "service_count": service_count,
        "domain_count": domain_count,
        "target_count": target_count,
        "running_target_count": int(vps.get("running_target_count") or 0),
        "monitoring_link_count": monitoring_link_count,
        "running_monitoring_count": int(vps.get("running_monitoring_instance_count") or 0),
        "abnormal_monitoring_count": abnormal_monitoring_count,
        "active_incident_count": active_incident_count,
        "primary_issue_summary": (
            "legacy service still responds on cancelled host"
            if vps_id == "vps_fra_legacy"
            else ""
        ),
        "cancellation_attention_reason": cancellation_attention_reason or "",
        "suggested_role": suggested_role,
        "suggested_action": suggested_action,
        "evidence_chips": evidence_chips,
        "evidence_assessment": assessment,
        "renewal_within_window": (
            (asset_decision_days_until(primary_subscription.get("renew_at")) or 9999) <= 30
            if primary_subscription
            else False
        ),
        "source_availability": asset_decision_source_availability(),
    }


def asset_decision_member_comparison(
    member: dict[str, object],
    *,
    rank: int,
) -> dict[str, object]:
    suggested_role = str(member.get("suggested_role") or "")
    suggested_action = str(member.get("suggested_action") or "review")
    chips = [
        str(chip.get("kind") or "")
        for chip in member.get("evidence_chips", [])
        if isinstance(chip, dict)
    ]
    vps = member.get("vps")
    if not isinstance(vps, dict):
        lane = "review"
        axis = "review"
        summary = "当前事实缺失，需要回到记录中复核"
    elif suggested_action == "complete_evidence" or any(kind.startswith("missing_") for kind in chips):
        lane = "evidence"
        axis = "evidence"
        summary = "证据缺口明显，先补齐订阅、监控或基础资料"
    elif suggested_action in {"open_cancellation_workbench", "cancel"}:
        lane = "retire"
        axis = "lifecycle"
        summary = "退役候选，优先核对取消联动和运行对象"
    elif suggested_action == "migrate":
        lane = "observe"
        axis = "renewal"
        summary = "迁移候选，重点看续费窗口与旧承载清理"
    elif suggested_role == "primary_candidate":
        lane = "primary"
        axis = "service_context"
        summary = "主力候选，承载和证据较完整"
    elif suggested_role == "standby_candidate":
        lane = "standby"
        axis = "monitoring"
        summary = "备用候选，适合保留为容灾或观察"
    else:
        lane = "review"
        axis = "review"
        summary = "需要结合成本、承载和证据继续复核"
    signals: list[dict[str, object]] = []
    if "budget_risk" in chips:
        signals.append(asset_decision_chip("budget_risk", "预算压力", "critical"))
    if "carries_service" in chips or int(member.get("service_count") or 0) > 0:
        signals.append(asset_decision_chip("service_context", "承载服务", "notice"))
    if "missing_subscription" in chips:
        signals.append(asset_decision_chip("missing_subscription", "缺订阅", "alert"))
    if int(member.get("monitoring_link_count") or 0) == 0:
        signals.append(asset_decision_chip("missing_monitoring", "未关联监控", "alert"))
    return {
        "rank": rank,
        "lane": lane,
        "primary_axis": axis,
        "summary": summary,
        "signals": signals,
    }


def asset_decision_assign_comparison(members: list[dict[str, object]]) -> None:
    lane_order = {
        "primary": 1,
        "standby": 2,
        "observe": 3,
        "retire": 4,
        "evidence": 5,
        "review": 6,
    }
    staged: list[tuple[int, dict[str, object]]] = []
    for index, member in enumerate(members):
        comparison = asset_decision_member_comparison(member, rank=0)
        staged.append((lane_order.get(str(comparison["lane"]), 99) * 100 + index, member))
        member["comparison_insight"] = comparison
    staged.sort(key=lambda item: item[0])
    for rank, (_, member) in enumerate(staged, start=1):
        comparison = member.get("comparison_insight")
        if isinstance(comparison, dict):
            comparison["rank"] = rank


def asset_decision_group_comparison(
    members: list[dict[str, object]],
    group_type: str,
) -> dict[str, object]:
    lane_counts: dict[str, int] = {}
    axis_counts: dict[str, int] = {}
    priority_ids: list[str] = []
    tradeoffs: list[dict[str, object]] = []
    for member in members:
        comparison = member.get("comparison_insight")
        vps = member.get("vps")
        if not isinstance(comparison, dict) or not isinstance(vps, dict):
            continue
        lane = str(comparison.get("lane") or "review")
        axis = str(comparison.get("primary_axis") or "review")
        lane_counts[lane] = lane_counts.get(lane, 0) + 1
        axis_counts[axis] = axis_counts.get(axis, 0) + 1
        if len(priority_ids) < 3:
            priority_ids.append(str(vps.get("vps_id") or ""))
        signals = comparison.get("signals")
        if isinstance(signals, list):
            for signal in signals:
                if isinstance(signal, dict) and len(tradeoffs) < 4:
                    tradeoffs.append(signal)
    primary_axis = max(axis_counts.items(), key=lambda item: item[1])[0] if axis_counts else "review"
    if group_type == "evidence_gap":
        summary = "资料缺口主导，先补齐证据再做组合取舍"
    elif group_type == "cancellation_attention":
        summary = "取消联动主导，先确认退役闭环"
    elif group_type == "cost_pressure":
        summary = "预算压力主导，比较保留价值和续费窗口"
    else:
        summary = "按主力、备用、观察、退役分层比较资产组合"
    lane_order = ["primary", "standby", "observe", "retire", "evidence", "review"]
    return {
        "primary_axis": primary_axis,
        "summary": summary,
        "lane_counts": [
            {"lane": lane, "count": lane_counts[lane]}
            for lane in lane_order
            if lane_counts.get(lane, 0) > 0
        ],
        "priority_vps_ids": [vps_id for vps_id in priority_ids if vps_id],
        "tradeoffs": tradeoffs,
    }


def asset_decision_count_by(rows: list[dict[str, object]], key: str) -> dict[str, int]:
    counts: dict[str, int] = {}
    for row in rows:
        value = str(row.get(key) or "unknown")
        counts[value] = counts.get(value, 0) + 1
    return counts


def asset_decision_cost_by_currency(vps_ids: list[str]) -> list[dict[str, object]]:
    by_currency: dict[str, dict[str, object]] = {}
    for row in asset_workflow_subscriptions():
        if row["vps_id"] not in vps_ids or row["status"] != "active":
            continue
        currency = str(row["currency"])
        item = by_currency.setdefault(
            currency,
            {"currency": currency, "monthly_total": 0.0, "yearly_total": 0.0},
        )
        item["monthly_total"] = round(float(item["monthly_total"]) + float(row.get("monthly_price") or 0), 2)
        item["yearly_total"] = round(float(item["yearly_total"]) + float(row.get("monthly_price") or 0) * 12, 2)
    return list(by_currency.values())


def asset_decision_group_summary(
    *,
    group_id: str,
    group_type: str,
    view: str,
    title: str,
    scope_key: str,
    scope_label: str,
    priority: int,
    vps_ids: list[str],
    evidence_chips: list[dict[str, object]],
    primary_issue_summary: str = "",
) -> dict[str, object]:
    vps_rows = [
        row
        for row in asset_workflow_vps_assets()
        if row["vps_id"] in vps_ids
    ]
    members = [
        asset_decision_member(
            str(row["vps_id"]),
            suggested_role="observe_candidate",
            suggested_action="review",
            evidence_chips=[],
        )
        for row in vps_rows
    ]
    monthly_cost_base = sum(
        float(row.get("monthly_price_base") or 0)
        for row in asset_workflow_subscriptions()
        if row["vps_id"] in vps_ids and row["status"] == "active"
    )
    yearly_cost_base = sum(
        float(row.get("yearly_price_base") or 0)
        for row in asset_workflow_subscriptions()
        if row["vps_id"] in vps_ids and row["status"] == "active"
    )
    assessment = asset_decision_group_assessment(members, group_type)
    asset_decision_assign_comparison(members)
    summary = {
        "group_id": group_id,
        "group_type": group_type,
        "view": view,
        "title": title,
        "scope_key": scope_key,
        "scope_label": scope_label,
        "priority": priority,
        "member_count": len(vps_rows),
        "lifecycle_counts": asset_decision_count_by(vps_rows, "lifecycle_status"),
        "usage_counts": asset_decision_count_by(vps_rows, "usage_status"),
        "renewal_decision_counts": asset_decision_count_by(vps_rows, "renewal_decision"),
        "renewal_window_count": sum(
            1
            for row in members
            if row["renewal_within_window"]
        ),
        "unreviewed_count": sum(1 for row in vps_rows if row["renewal_decision"] == "unreviewed"),
        "migrate_count": sum(1 for row in vps_rows if row["renewal_decision"] == "migrate"),
        "cancel_count": sum(1 for row in vps_rows if row["renewal_decision"] == "cancel"),
        "cancellation_attention_count": sum(
            1
            for row in vps_rows
            if row["lifecycle_status"] in {"to_cancel", "cancelled"} or row["renewal_decision"] == "cancel"
        ),
        "idle_count": sum(1 for row in vps_rows if row["usage_status"] == "idle"),
        "standby_count": sum(1 for row in vps_rows if row["usage_status"] == "standby"),
        "in_use_count": sum(1 for row in vps_rows if row["usage_status"] == "in_use"),
        "service_count": sum(int(row["service_count"]) for row in members),
        "domain_count": sum(int(row["domain_count"]) for row in members),
        "target_count": sum(int(row["target_count"]) for row in members),
        "running_target_count": sum(int(row["running_target_count"]) for row in members),
        "monitoring_link_count": sum(int(row["monitoring_link_count"]) for row in members),
        "abnormal_monitoring_count": sum(int(row["abnormal_monitoring_count"]) for row in members),
        "active_incident_count": sum(int(row["active_incident_count"]) for row in members),
        "primary_issue_summary": primary_issue_summary,
        "monthly_cost_by_currency": asset_decision_cost_by_currency(vps_ids),
        "monthly_cost_base": round(monthly_cost_base, 2),
        "yearly_cost_base": round(yearly_cost_base, 2),
        "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
        "evidence_chips": evidence_chips,
        "evidence_assessment": assessment,
    }
    summary["comparison_insight"] = asset_decision_group_comparison(members, group_type)
    return summary


def asset_decision_group_definitions() -> list[dict[str, object]]:
    definitions = [
        {
            "vps_ids": ["vps_ams_core", "vps_sjc_edge", "vps_fra_legacy"],
            "summary": asset_decision_group_summary(
                group_id="adg_auto_mock_renewal",
                group_type="renewal_attention",
                view="renewal",
                title="续费窗口组合取舍",
                scope_key="renewal-window",
                scope_label="未来 30 天",
                priority=94,
                vps_ids=["vps_ams_core", "vps_sjc_edge", "vps_fra_legacy"],
                evidence_chips=[
                    asset_decision_chip("renewal_due", "续费临近", "alert"),
                    asset_decision_chip("budget_risk", "预算风险", "critical"),
                ],
            ),
            "members": [
                asset_decision_member(
                    "vps_ams_core",
                    suggested_role="primary_candidate",
                    suggested_action="keep",
                    evidence_chips=[
                        asset_decision_chip("renewal_due", "8 天后续费", "alert"),
                        asset_decision_chip("budget_risk", "超预算", "critical"),
                    ],
                ),
                asset_decision_member(
                    "vps_sjc_edge",
                    suggested_role="standby_candidate",
                    suggested_action="migrate",
                    evidence_chips=[
                        asset_decision_chip("renewal_due", "21 天后续费", "notice"),
                        asset_decision_chip("exchange_rate_stale", "汇率过期", "alert"),
                    ],
                ),
                asset_decision_member(
                    "vps_fra_legacy",
                    suggested_role="retire_candidate",
                    suggested_action="open_cancellation_workbench",
                    evidence_chips=[
                        asset_decision_chip("cancellation_linkage", "取消联动", "critical"),
                        asset_decision_chip("carries_service", "仍承载服务", "alert"),
                    ],
                    cancellation_attention_reason="VPS 待取消，但仍有关联监控和 Target 运行。",
                ),
            ],
        },
        {
            "vps_ids": ["vps_fra_legacy"],
            "summary": asset_decision_group_summary(
                group_id="adg_auto_mock_cancel",
                group_type="cancellation_attention",
                view="needs_decision",
                title="取消联动待确认",
                scope_key="cancellation-linkage",
                scope_label="取消 / 运行状态割裂",
                priority=99,
                vps_ids=["vps_fra_legacy"],
                evidence_chips=[
                    asset_decision_chip("cancellation_linkage", "取消联动", "critical"),
                    asset_decision_chip("abnormal_monitoring", "异常关联", "alert"),
                ],
                primary_issue_summary="legacy service still responds on cancelled host",
            ),
            "members": [
                asset_decision_member(
                    "vps_fra_legacy",
                    suggested_role="retire_candidate",
                    suggested_action="open_cancellation_workbench",
                    evidence_chips=[
                        asset_decision_chip("cancellation_linkage", "取消联动", "critical"),
                        asset_decision_chip("carries_service", "仍承载服务", "alert"),
                    ],
                    cancellation_attention_reason="待取消 VPS 下仍有运行中的 MonitoringInstance 与 Target。",
                )
            ],
        },
        {
            "vps_ids": ["vps_ams_core", "vps_fra_legacy"],
            "summary": asset_decision_group_summary(
                group_id="adg_auto_mock_region_eu",
                group_type="region_portfolio",
                view="region",
                title="欧洲节点组合比较",
                scope_key="region:eu",
                scope_label="EU / Europe",
                priority=70,
                vps_ids=["vps_ams_core", "vps_fra_legacy"],
                evidence_chips=[
                    asset_decision_chip("carries_service", "服务承载差异", "notice"),
                    asset_decision_chip("idle_paid", "闲置付费", "alert"),
                ],
            ),
            "members": [
                asset_decision_member(
                    "vps_ams_core",
                    suggested_role="primary_candidate",
                    suggested_action="keep",
                    evidence_chips=[asset_decision_chip("renewal_due", "续费临近", "alert")],
                ),
                asset_decision_member(
                    "vps_fra_legacy",
                    suggested_role="retire_candidate",
                    suggested_action="open_cancellation_workbench",
                    evidence_chips=[asset_decision_chip("idle_paid", "闲置付费", "alert")],
                ),
            ],
        },
        {
            "vps_ids": ["vps_ams_core", "vps_fra_legacy"],
            "summary": asset_decision_group_summary(
                group_id="adg_auto_mock_provider",
                group_type="provider_portfolio",
                view="provider",
                title="服务商组合复核",
                scope_key="provider:mixed-eu",
                scope_label="Hetzner / Netcup",
                priority=66,
                vps_ids=["vps_ams_core", "vps_fra_legacy"],
                evidence_chips=[
                    asset_decision_chip("budget_risk", "成本差异", "alert"),
                    asset_decision_chip("abnormal_monitoring", "服务质量线索", "notice"),
                ],
            ),
            "members": [
                asset_decision_member(
                    "vps_ams_core",
                    suggested_role="primary_candidate",
                    suggested_action="keep",
                    evidence_chips=[asset_decision_chip("budget_risk", "预算风险", "critical")],
                ),
                asset_decision_member(
                    "vps_fra_legacy",
                    suggested_role="retire_candidate",
                    suggested_action="open_cancellation_workbench",
                    evidence_chips=[asset_decision_chip("abnormal_monitoring", "异常关联", "alert")],
                ),
            ],
        },
        {
            "vps_ids": ["vps_ams_core", "vps_sjc_edge"],
            "summary": asset_decision_group_summary(
                group_id="adg_auto_mock_cost",
                group_type="cost_pressure",
                view="cost",
                title="预算压力组合",
                scope_key="cost:budget",
                scope_label="CNY 预算风险",
                priority=86,
                vps_ids=["vps_ams_core", "vps_sjc_edge"],
                evidence_chips=[
                    asset_decision_chip("budget_risk", "超预算 / 预警", "critical"),
                    asset_decision_chip("exchange_rate_stale", "汇率过期", "alert"),
                ],
            ),
            "members": [
                asset_decision_member(
                    "vps_ams_core",
                    suggested_role="primary_candidate",
                    suggested_action="review",
                    evidence_chips=[asset_decision_chip("budget_risk", "超预算", "critical")],
                ),
                asset_decision_member(
                    "vps_sjc_edge",
                    suggested_role="observe_candidate",
                    suggested_action="migrate",
                    evidence_chips=[asset_decision_chip("exchange_rate_stale", "汇率过期", "alert")],
                ),
            ],
        },
        {
            "vps_ids": ["vps_tokyo_lab"],
            "summary": asset_decision_group_summary(
                group_id="adg_auto_mock_evidence",
                group_type="evidence_gap",
                view="evidence",
                title="资料缺口待补齐",
                scope_key="evidence-gap",
                scope_label="缺订阅 / 缺监控 / 缺基础资料",
                priority=78,
                vps_ids=["vps_tokyo_lab"],
                evidence_chips=[
                    asset_decision_chip("missing_subscription", "缺订阅", "alert"),
                    asset_decision_chip("missing_monitoring", "未关联监控", "alert"),
                    asset_decision_chip("missing_access", "缺访问入口", "notice"),
                ],
            ),
            "members": [
                asset_decision_member(
                    "vps_tokyo_lab",
                    suggested_role="evidence_needed",
                    suggested_action="complete_evidence",
                    evidence_chips=[
                        asset_decision_chip("missing_subscription", "缺订阅", "alert"),
                        asset_decision_chip("missing_monitoring", "未关联监控", "alert"),
                        asset_decision_chip("missing_provider", "缺服务商", "notice"),
                        asset_decision_chip("missing_location", "缺地域", "notice"),
                        asset_decision_chip("missing_access", "缺访问入口", "notice"),
                    ],
                )
            ],
        },
    ]
    for definition in definitions:
        members = definition.get("members")
        summary = definition.get("summary")
        if isinstance(members, list) and isinstance(summary, dict):
            asset_decision_assign_comparison(members)
            summary["comparison_insight"] = asset_decision_group_comparison(
                members,
                str(summary.get("group_type") or "review"),
            )
    return definitions


def asset_decision_public_groups(query: dict[str, list[str]]) -> list[dict[str, object]]:
    view = first_query_value(query, "view") or "needs_decision"
    groups = [dict(item["summary"]) for item in asset_decision_group_definitions()]
    if view == "needs_decision":
        allowed_types = {"renewal_attention", "cancellation_attention", "cost_pressure", "evidence_gap"}
        groups = [group for group in groups if str(group["group_type"]) in allowed_types]
    elif view:
        groups = [group for group in groups if group["view"] == view]
    groups.sort(key=lambda group: (-int(group["priority"]), str(group["title"])))
    return groups


def asset_decision_overview(query: dict[str, list[str]]) -> dict[str, object]:
    all_groups = [dict(item["summary"]) for item in asset_decision_group_definitions()]
    type_counts: dict[str, int] = {}
    view_counts: dict[str, int] = {}
    member_ids: set[str] = set()
    for definition in asset_decision_group_definitions():
        summary = definition["summary"]
        group_type = str(summary["group_type"])
        view = str(summary["view"])
        type_counts[group_type] = type_counts.get(group_type, 0) + 1
        view_counts[view] = view_counts.get(view, 0) + 1
        member_ids.update(str(vps_id) for vps_id in definition["vps_ids"])
    all_groups.sort(key=lambda group: -int(group["priority"]))
    return {
        "snapshot_generated_at": iso_timestamp(0),
        "renew_within_days": int(first_query_value(query, "renew_within_days") or "30"),
        "group_count": len(all_groups),
        "member_vps_count": len(member_ids),
        "needs_decision_count": len([
            group
            for group in all_groups
            if group["group_type"] in {"renewal_attention", "cancellation_attention", "cost_pressure", "evidence_gap"}
        ]),
        "renewal_group_count": view_counts.get("renewal", 0),
        "region_group_count": view_counts.get("region", 0),
        "provider_group_count": view_counts.get("provider", 0),
        "cost_group_count": view_counts.get("cost", 0),
        "evidence_group_count": view_counts.get("evidence", 0),
        "top_groups": all_groups[:4],
        "type_counts": type_counts,
        "view_counts": view_counts,
        "source_availability": asset_decision_source_availability(),
    }


def asset_decision_group_detail(group_id: str) -> dict[str, object] | None:
    for definition in asset_decision_group_definitions():
        summary = dict(definition["summary"])
        if summary["group_id"] == group_id:
            summary["members"] = definition["members"]
            return summary
    return None


def asset_decision_manual_group_member(
    member: dict[str, object],
    *,
    manual_group_id: str,
    intended_role: str,
    intended_action: str,
    reason: str,
    note: str,
    sort_order: int,
) -> dict[str, object]:
    vps = member["vps"]
    assert isinstance(vps, dict)
    return {
        **member,
        "manual_group_id": manual_group_id,
        "vps_id": vps["vps_id"],
        "intended_role": intended_role,
        "intended_action": intended_action,
        "reason": reason,
        "note": note,
        "sort_order": sort_order,
        "evidence_snapshot": {
            "vps_id": vps["vps_id"],
            "display_name": vps["display_name"],
            "provider_name": vps.get("provider_name", ""),
            "lifecycle_status": vps.get("lifecycle_status", ""),
            "usage_status": vps.get("usage_status", ""),
            "renewal_decision": vps.get("renewal_decision", ""),
            "service_count": member.get("service_count", 0),
            "domain_count": member.get("domain_count", 0),
            "target_count": member.get("target_count", 0),
            "monitoring_link_count": member.get("monitoring_link_count", 0),
            "evidence_chips": member.get("evidence_chips", []),
            "evidence_assessment": member.get("evidence_assessment", {}),
        },
        "current_fact_found": True,
        "created_at": iso_timestamp(-1),
        "updated_at": iso_timestamp(-1),
    }


def asset_decision_manual_group_detail(
    manual_group_id: str = "admg_mock_primary_standby",
) -> dict[str, object] | None:
    if manual_group_id not in {"admg_mock_primary_standby", "admg_mock_created"}:
        return None
    group = asset_decision_group_detail("adg_auto_mock_region_eu")
    if group is None:
        return None
    members_by_id = {str(member["vps"]["vps_id"]): member for member in group["members"]}
    members = [
        asset_decision_manual_group_member(
            members_by_id["vps_ams_core"],
            manual_group_id=manual_group_id,
            intended_role="primary_candidate",
            intended_action="keep",
            reason="欧洲主力节点，承载核心服务。",
            note="续费前确认预算。",
            sort_order=10,
        ),
        asset_decision_manual_group_member(
            members_by_id["vps_fra_legacy"],
            manual_group_id=manual_group_id,
            intended_role="retire_candidate",
            intended_action="open_cancellation_workbench",
            reason="同区旧节点成本价值偏低，需先清理联动。",
            note="等待服务迁移完成。",
            sort_order=20,
        ),
    ]
    asset_decision_assign_comparison(members)
    summary = asset_decision_manual_group_summary(manual_group_id, members)
    summary["members"] = members
    return summary


def asset_decision_manual_group_summary(
    manual_group_id: str,
    members: list[dict[str, object]] | None = None,
) -> dict[str, object]:
    if members is None:
        detail = asset_decision_manual_group_detail(manual_group_id)
        if detail is not None:
            return {key: value for key, value in detail.items() if key != "members"}
        members = []
    vps_ids = [
        str(member["vps"]["vps_id"])
        for member in members
        if isinstance(member.get("vps"), dict)
    ]
    base = asset_decision_group_summary(
        group_id="adg_manual_mock_region_eu",
        group_type="region_portfolio",
        view="region",
        title="欧洲主备手工组合",
        scope_key="manual:primary-standby-eu",
        scope_label="用户场景 / 欧洲主备",
        priority=90,
        vps_ids=vps_ids,
        evidence_chips=[
            asset_decision_chip("carries_service", "承载服务", "notice"),
            asset_decision_chip("cancellation_linkage", "取消联动", "critical"),
        ],
    )
    return {
        "manual_group_id": manual_group_id,
        "status": "active",
        "scenario": "primary_standby",
        "title": "欧洲主备手工组合",
        "goal": "确认欧洲主力、备用与退役节点的保留顺序。",
        "note": "用户从同区自动组沉淀出的真实决策篮子。",
        "source_type": "auto_group",
        "source_group_id": "adg_auto_mock_region_eu",
        "source_group_type": "region_portfolio",
        "source_view": "region",
        "scope_key": "manual:primary-standby-eu",
        "scope_label": "用户场景 / 欧洲主备",
        "renew_within_days": 30,
        "member_count": len(vps_ids),
        "lifecycle_counts": base["lifecycle_counts"],
        "usage_counts": base["usage_counts"],
        "renewal_decision_counts": base["renewal_decision_counts"],
        "renewal_window_count": base["renewal_window_count"],
        "unreviewed_count": base["unreviewed_count"],
        "migrate_count": base["migrate_count"],
        "cancel_count": base["cancel_count"],
        "cancellation_attention_count": base["cancellation_attention_count"],
        "idle_count": base["idle_count"],
        "standby_count": base["standby_count"],
        "in_use_count": base["in_use_count"],
        "service_count": base["service_count"],
        "domain_count": base["domain_count"],
        "target_count": base["target_count"],
        "running_target_count": base["running_target_count"],
        "monitoring_link_count": base["monitoring_link_count"],
        "abnormal_monitoring_count": base["abnormal_monitoring_count"],
        "active_incident_count": base["active_incident_count"],
        "primary_issue_summary": "旧节点仍有关联运行对象，保存决策前需明确退役入口。",
        "monthly_cost_by_currency": base["monthly_cost_by_currency"],
        "monthly_cost_base": base["monthly_cost_base"],
        "yearly_cost_base": base["yearly_cost_base"],
        "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
        "evidence_chips": base["evidence_chips"],
        "evidence_assessment": base["evidence_assessment"],
        "comparison_insight": asset_decision_group_comparison(members, "region_portfolio"),
        "source_availability": asset_decision_source_availability(),
        "created_at": iso_timestamp(-1),
        "updated_at": iso_timestamp(-1),
        "archived_at": None,
    }


def asset_decision_manual_groups() -> list[dict[str, object]]:
    return [asset_decision_manual_group_summary("admg_mock_primary_standby")]


def asset_decision_scenario_template_member(
    member: dict[str, object],
    *,
    template_id: str,
    sort_order: int,
) -> dict[str, object]:
    vps = member["vps"]
    assert isinstance(vps, dict)
    return {
        "template_id": template_id,
        "vps_id": vps["vps_id"],
        "display_name": vps["display_name"],
        "intended_role": member.get("suggested_role", "observe_candidate"),
        "intended_action": member.get("suggested_action", "review"),
        "reason": "从代表性资产事实生成的场景成员蓝图。",
        "note": "创建组合时后端会重新读取当前事实。",
        "sort_order": sort_order,
    }


def asset_decision_scenario_template_detail(
    template_id: str = "adt_builtin_primary_standby",
) -> dict[str, object] | None:
    manual = asset_decision_manual_group_detail()
    assert manual is not None
    members = list(manual.get("members", []))
    if template_id == "adt_builtin_primary_standby":
        blueprint = [
            asset_decision_scenario_template_member(
                member,
                template_id=template_id,
                sort_order=index * 10,
            )
            for index, member in enumerate(members, start=1)
            if isinstance(member, dict)
        ]
        return {
            "template_id": template_id,
            "builtin": True,
            "status": "active",
            "scenario": "primary_standby",
            "title": "主备取舍模板",
            "goal": "比较主力、备用与可退役 VPS，先形成自定义组合再保存决策。",
            "note": "内置模板只保存场景 blueprint，不保存当前成本、订阅或监控事实。",
            "source_manual_group_id": None,
            "member_count": len(blueprint),
            "created_at": iso_timestamp(-1),
            "updated_at": iso_timestamp(-1),
            "archived_at": None,
            "members": blueprint,
        }
    if template_id == "adt_mock_manual_primary_standby":
        blueprint = [
            asset_decision_scenario_template_member(
                member,
                template_id=template_id,
                sort_order=index * 10,
            )
            for index, member in enumerate(members, start=1)
            if isinstance(member, dict)
        ]
        return {
            "template_id": template_id,
            "builtin": False,
            "status": "active",
            "scenario": "primary_standby",
            "title": "欧洲主备手工组合 模板",
            "goal": str(manual.get("goal") or ""),
            "note": "从手工组合另存的模板 fixture。",
            "source_manual_group_id": manual["manual_group_id"],
            "member_count": len(blueprint),
            "created_at": iso_timestamp(-1),
            "updated_at": iso_timestamp(0),
            "archived_at": None,
            "members": blueprint,
        }
    return None


def asset_decision_scenario_templates() -> list[dict[str, object]]:
    rows: list[dict[str, object]] = []
    for template_id in ["adt_builtin_primary_standby", "adt_mock_manual_primary_standby"]:
        detail = asset_decision_scenario_template_detail(template_id)
        assert detail is not None
        rows.append({key: value for key, value in detail.items() if key != "members"})
    return rows


def asset_decision_record_summary(
    record_id: str = "adr_mock_eu_renewal",
    members: list[dict[str, object]] | None = None,
) -> dict[str, object]:
    group = asset_decision_group_detail("adg_auto_mock_renewal")
    assert group is not None
    member_count = len(group["members"])
    readback = asset_decision_record_readback(members or [])
    plan = asset_decision_record_execution_plan(members or [], readback)
    return {
        "record_id": record_id,
        "title": "欧洲主备节点续费决策",
        "goal": "保留 ams-core-01 作为主力，迁移 sjc-edge-02，进入 fra-legacy-cancel 取消联动检查。",
        "status": "decided",
        "source_type": "auto_group",
        "source_group_id": group["group_id"],
        "source_group_type": group["group_type"],
        "source_view": group["view"],
        "scope_key": group["scope_key"],
        "scope_label": group["scope_label"],
        "renew_within_days": 30,
        "member_count": member_count,
        "followup_todo_count": 1,
        "followup_in_progress_count": 1 if member_count > 1 else 0,
        "followup_blocked_count": 1 if member_count > 2 else 0,
        "followup_done_count": 0,
        "followup_skipped_count": 0,
        "evidence_snapshot": asset_decision_record_group_snapshot(group),
        "execution_readback": readback,
        "execution_plan": plan,
        "created_at": iso_timestamp(-2),
        "updated_at": iso_timestamp(-1),
        "decided_at": iso_timestamp(-1),
        "completed_at": None,
    }


def asset_decision_record_group_snapshot(group: dict[str, object]) -> dict[str, object]:
    keys = [
        "group_id",
        "group_type",
        "view",
        "title",
        "scope_key",
        "scope_label",
        "member_count",
        "renewal_window_count",
        "unreviewed_count",
        "migrate_count",
        "cancel_count",
        "cancellation_attention_count",
        "idle_count",
        "standby_count",
        "in_use_count",
        "service_count",
        "domain_count",
        "target_count",
        "running_target_count",
        "monitoring_link_count",
        "abnormal_monitoring_count",
        "active_incident_count",
        "primary_issue_summary",
        "monthly_cost_by_currency",
        "monthly_cost_base",
        "yearly_cost_base",
        "base_currency",
        "evidence_chips",
        "evidence_assessment",
        "comparison_insight",
        "source_availability",
    ]
    return {key: group[key] for key in keys if key in group}


def asset_decision_record_member(
    member: dict[str, object],
    *,
    record_id: str,
    decided_role: str,
    decided_action: str,
    reason: str,
    followup_status: str = "todo",
    followup_note: str = "",
) -> dict[str, object]:
    vps = member["vps"]
    assert isinstance(vps, dict)
    vps_id = str(vps["vps_id"])
    display_name = str(vps["display_name"])
    evidence_snapshot = {
        "vps_id": vps_id,
        "display_name": display_name,
        "provider_name": vps.get("provider_name", ""),
        "country": vps.get("country", ""),
        "region": vps.get("region", ""),
        "city": vps.get("city", ""),
        "lifecycle_status": vps.get("lifecycle_status", ""),
        "usage_status": vps.get("usage_status", ""),
        "renewal_decision": vps.get("renewal_decision", ""),
        "subscription_count": member.get("subscription_count", 0),
        "active_subscription_count": member.get("active_subscription_count", 0),
        "inactive_subscription_count": member.get("inactive_subscription_count", 0),
        "service_count": member.get("service_count", 0),
        "domain_count": member.get("domain_count", 0),
        "target_count": member.get("target_count", 0),
        "running_target_count": member.get("running_target_count", 0),
        "monitoring_link_count": member.get("monitoring_link_count", 0),
        "running_monitoring_count": member.get("running_monitoring_count", 0),
        "abnormal_monitoring_count": member.get("abnormal_monitoring_count", 0),
        "active_incident_count": member.get("active_incident_count", 0),
        "primary_issue_summary": member.get("primary_issue_summary", ""),
        "cancellation_attention_reason": member.get("cancellation_attention_reason", ""),
        "renewal_within_window": member.get("renewal_within_window", False),
        "evidence_chips": member.get("evidence_chips", []),
        "evidence_assessment": member.get("evidence_assessment", {}),
        "comparison_insight": member.get("comparison_insight", {}),
        "source_availability": member.get("source_availability", {}),
    }
    if member.get("primary_subscription") is not None:
        evidence_snapshot["primary_subscription"] = member["primary_subscription"]
    readback = asset_decision_member_readback(
        member,
        decided_action,
        followup_status,
    )
    record_member = {
        "record_id": record_id,
        "vps_id": vps_id,
        "display_name": display_name,
        "suggested_role": member["suggested_role"],
        "decided_role": decided_role,
        "suggested_action": member["suggested_action"],
        "decided_action": decided_action,
        "reason": reason,
        "followup_status": followup_status,
        "followup_note": followup_note,
        "followup_updated_at": iso_timestamp(-1) if followup_status != "todo" else None,
        "evidence_snapshot": evidence_snapshot,
        "execution_readback": readback,
        "created_at": iso_timestamp(-2),
        "updated_at": iso_timestamp(-1),
    }
    record_member["execution_plan"] = asset_decision_member_execution_plan(record_member)
    return record_member


def asset_decision_record_detail(record_id: str = "adr_mock_eu_renewal") -> dict[str, object] | None:
    if record_id not in {"adr_mock_eu_renewal", "adr_mock_created"}:
        return None
    group = asset_decision_group_detail("adg_auto_mock_renewal")
    if group is None:
        return None
    members_by_id = {str(member["vps"]["vps_id"]): member for member in group["members"]}
    members = [
        asset_decision_record_member(
            members_by_id["vps_ams_core"],
            record_id=record_id,
            decided_role="primary_candidate",
            decided_action="keep",
            reason="主力节点证据完整，虽然超预算但承载核心服务。",
        ),
        asset_decision_record_member(
            members_by_id["vps_sjc_edge"],
            record_id=record_id,
            decided_role="standby_candidate",
            decided_action="migrate",
            reason="作为备用节点继续观察迁移窗口。",
            followup_status="in_progress",
            followup_note="正在准备迁移窗口。",
        ),
        asset_decision_record_member(
            members_by_id["vps_fra_legacy"],
            record_id=record_id,
            decided_role="retire_candidate",
            decided_action="open_cancellation_workbench",
            reason="取消前需要先处理仍在运行的服务和监控联动。",
            followup_status="blocked",
            followup_note="等待服务迁移完成后进入取消工作台。",
        ),
    ]
    summary = asset_decision_record_summary(record_id, members)
    summary["members"] = members
    return summary


def asset_decision_records() -> list[dict[str, object]]:
    detail = asset_decision_record_detail()
    assert detail is not None
    return [{key: value for key, value in detail.items() if key != "members"}]


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


def asset_workflow_archive_review(vps_id: str) -> dict[str, object] | None:
    detail = asset_workflow_vps_detail(vps_id)
    if detail is None:
        return None
    subscriptions = [
        {
            "record": row,
            "role": "active" if row["status"] == "active" else "inactive",
            "recommended_action": (
                "cancel_subscription_first"
                if row["status"] == "active"
                else "read_only_history"
            ),
            "message": (
                "订阅仍为 active，不能归档。"
                if row["status"] == "active"
                else "历史订阅只读保留。"
            ),
        }
        for row in asset_workflow_subscriptions()
        if row["vps_id"] == vps_id
    ]
    services = asset_workflow_services(vps_id)
    domains = asset_workflow_domains(vps_id)
    target_links = []
    if vps_id == "vps_archive_old":
        target_links.append(
            {
                "target_id": "target_legacy_archived",
                "name": "archive-old.example.test",
                "run_status": "已归档",
                "service_ids": [service["service_id"] for service in services],
                "domain_ids": [domain["domain_id"] for domain in domains],
                "last_linked_at": iso_timestamp(-30),
            }
        )
    blockers = []
    if detail["lifecycle_status"] == "archived":
        blockers.append("VPS 已归档，只能在归档详情页只读查看或执行受控恢复。")
    active_subscription_count = sum(1 for row in subscriptions if row["record"]["status"] == "active")
    if active_subscription_count > 0:
        blockers.append(f"存在 {active_subscription_count} 条 active 订阅，必须先取消或结束订阅后才能归档。")
    return {
        "vps": detail,
        "subscriptions": subscriptions,
        "monitoring_instance_links": detail["monitoring_instance_links"],
        "services": services,
        "domains": domains,
        "target_links": target_links,
        "warnings": [],
        "blockers": blockers,
        "eligible": len(blockers) == 0,
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


def active_asset_workflow_subscriptions() -> list[dict[str, object]]:
    return [
        row
        for row in asset_workflow_subscriptions()
        if row.get("status") == "active"
    ]


def asset_workflow_budget_records() -> list[dict[str, object]]:
    return [
        {
            "budget_id": "budget_global",
            "scope_type": "global",
            "scope_id": "",
            "name": "全局 VPS 月预算",
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "monthly_limit": 150.0,
            "yearly_limit": None,
            "warning_pct": 80,
            "enabled": True,
            "note": "Visual evidence global budget.",
            "current_monthly_spend": 174.55,
            "current_yearly_spend": 2094.6,
            "status": "over",
            "created_at": iso_timestamp(-20),
            "updated_at": iso_timestamp(-1),
        },
        {
            "budget_id": "budget_edge",
            "scope_type": "label",
            "scope_id": "edge",
            "name": "Edge 节点预算",
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "monthly_limit": 60.0,
            "yearly_limit": None,
            "warning_pct": 80,
            "enabled": True,
            "note": "Edge fleet warning budget.",
            "current_monthly_spend": 58.4,
            "current_yearly_spend": 700.8,
            "status": "warning",
            "created_at": iso_timestamp(-15),
            "updated_at": iso_timestamp(-1),
        },
    ]


def asset_workflow_monthly_budget_records() -> list[dict[str, object]]:
    return [
        {
            "budget_month": f"{month_bucket(-9)}-01",
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "monthly_limit": 120.0,
            "warning_pct": 80,
            "note": "Initial visual evidence monthly budget.",
            "created_at": iso_timestamp(-270),
            "updated_at": iso_timestamp(-270),
        },
        {
            "budget_month": f"{month_bucket(-3)}-01",
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "monthly_limit": 150.0,
            "warning_pct": 80,
            "note": "Growth adjustment fixture.",
            "created_at": iso_timestamp(-90),
            "updated_at": iso_timestamp(-90),
        },
    ]


def asset_workflow_subscription_settings() -> dict[str, object]:
    return {
        "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
        "exchange_rate_provider": "frankfurter",
        "fixer_configured": False,
        "default_reminder_offsets_days": [14, 7, 1],
        "max_reminder_lead_days": 30,
        "exchange_rate_stale_after_hours": 36,
    }


def asset_workflow_missing_subscription_assets() -> list[dict[str, object]]:
    return [
        {
            "vps_id": "vps_tokyo_lab",
            "display_name": "tokyo-lab-unlinked",
            "provider_id": "",
            "provider_name": "",
            "lifecycle_status": "testing",
            "renewal_decision": "unreviewed",
        }
    ]


def subscription_breakdown(
    rows: list[dict[str, object]],
    key_fn: object,
) -> list[dict[str, object]]:
    items: dict[str, dict[str, object]] = {}
    for row in rows:
        monthly_cost = row.get("monthly_price_base")
        yearly_cost = row.get("yearly_price_base")
        if not isinstance(monthly_cost, (int, float)) or not isinstance(yearly_cost, (int, float)):
            continue
        key, label = key_fn(row)
        item = items.setdefault(
            str(key),
            {
                "key": str(key),
                "label": str(label),
                "monthly_cost": 0.0,
                "yearly_cost": 0.0,
                "subscription_count": 0,
            },
        )
        item["monthly_cost"] = float(item["monthly_cost"]) + float(monthly_cost)
        item["yearly_cost"] = float(item["yearly_cost"]) + float(yearly_cost)
        item["subscription_count"] = int(item["subscription_count"]) + 1
    return sorted(
        items.values(),
        key=lambda item: (-float(item["monthly_cost"]), str(item["label"])),
    )


def asset_workflow_provider_label(subscription: dict[str, object]) -> tuple[str, str]:
    vps = next(
        (row for row in asset_workflow_vps_assets() if row["vps_id"] == subscription["vps_id"]),
    )
    provider_id = str(vps.get("provider_id") or "")
    provider_name = str(vps.get("provider_name") or provider_id or "未记录服务商")
    return provider_id or provider_name, provider_name


def asset_workflow_vps_for_subscription(subscription: dict[str, object]) -> dict[str, object]:
    return next(
        (row for row in asset_workflow_vps_assets() if row["vps_id"] == subscription["vps_id"]),
    )


def asset_workflow_cost_rows(rows: list[dict[str, object]]) -> list[dict[str, object]]:
    costs: list[dict[str, object]] = []
    for row in rows:
        vps = asset_workflow_vps_for_subscription(row)
        cost = dict(row)
        cost["vps_display_name"] = vps.get("display_name", row.get("vps_id"))
        cost["provider_id"] = vps.get("provider_id", "")
        cost["provider_name"] = vps.get("provider_name", "")
        cost["country"] = vps.get("country", "")
        cost["region"] = vps.get("region", "")
        cost["lifecycle_status"] = vps.get("lifecycle_status", "")
        cost["renewal_decision"] = vps.get("renewal_decision", "")
        costs.append(cost)
    return costs


def asset_workflow_subscription_overview() -> dict[str, object]:
    rows = active_asset_workflow_subscriptions()
    cost_rows = asset_workflow_cost_rows(rows)
    total_monthly = sum(float(row.get("monthly_price_base") or 0) for row in rows)
    total_yearly = sum(float(row.get("yearly_price_base") or 0) for row in rows)
    budgets = asset_workflow_budget_records()
    upcoming = [
        {
            "subscription_id": row["subscription_id"],
            "vps_id": row["vps_id"],
            "vps_display_name": next(
                vps["display_name"]
                for vps in asset_workflow_vps_assets()
                if vps["vps_id"] == row["vps_id"]
            ),
            "display_name": row.get("display_name", ""),
            "provider_name": asset_workflow_provider_label(row)[1],
            "renew_at": row.get("renew_at"),
            "monthly_price_base": row.get("monthly_price_base"),
            "yearly_price_base": row.get("yearly_price_base"),
            "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
            "currency": row.get("currency"),
            "renewal_decision": next(
                vps["renewal_decision"]
                for vps in asset_workflow_vps_assets()
                if vps["vps_id"] == row["vps_id"]
            ),
            "lifecycle_status": next(
                vps["lifecycle_status"]
                for vps in asset_workflow_vps_assets()
                if vps["vps_id"] == row["vps_id"]
            ),
            "exchange_rate_stale": row.get("exchange_rate_stale", False),
        }
        for row in rows
    ]
    upcoming.sort(key=lambda row: str(row.get("renew_at") or "9999-12-31"))
    return {
        "snapshot_generated_at": iso_timestamp(0),
        "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
        "total_monthly_cost": round(total_monthly, 2),
        "total_yearly_cost": round(total_yearly, 2),
        "active_subscription_count": len(rows),
        "renewal_due_14d_count": 2,
        "renewal_due_30d_count": 3,
        "budget_risk_count": 2,
        "exchange_rate_stale_count": 1,
        "decision_attention_count": 2,
        "missing_subscription_vps_count": 1,
        "upcoming_renewals": upcoming,
        "provider_breakdown": subscription_breakdown(rows, asset_workflow_provider_label),
        "currency_breakdown": subscription_breakdown(
            rows,
            lambda row: (str(row["currency"]), str(row["currency"])),
        ),
        "category_breakdown": subscription_breakdown(
            rows,
            lambda row: (
                str(row.get("cost_category") or "未分类"),
                str(row.get("cost_category") or "未分类"),
            ),
        ),
        "payment_breakdown": subscription_breakdown(
            rows,
            lambda row: (
                str(row.get("payment_method") or "未记录"),
                str(row.get("payment_method") or "未记录"),
            ),
        ),
        "region_breakdown": subscription_breakdown(
            cost_rows,
            lambda row: (
                str(row.get("country") or row.get("region") or "未记录"),
                str(row.get("country") or row.get("region") or "未记录"),
            ),
        ),
        "budget_risks": budgets,
        "vps_costs": cost_rows,
        "missing_subscription_assets": asset_workflow_missing_subscription_assets(),
    }


def asset_workflow_subscription_statistics(window: str | None = None) -> dict[str, object]:
    overview = asset_workflow_subscription_overview()
    cost_months = [72.5, 83.0, 83.0, 101.4, 111.9, 111.9, 135.6, 135.6, 151.3, 151.3, 174.55, 174.55]
    budget_months = [None, None, 120.0, 120.0, 120.0, 120.0, 120.0, 120.0, 150.0, 150.0, 150.0, 150.0]
    return {
        "window": window if window in {"month", "quarter", "year"} else "month",
        "base_currency": ASSET_WORKFLOW_BASE_CURRENCY,
        "total_monthly_cost": overview["total_monthly_cost"],
        "total_yearly_cost": overview["total_yearly_cost"],
        "provider_breakdown": overview["provider_breakdown"],
        "currency_breakdown": overview["currency_breakdown"],
        "category_breakdown": overview["category_breakdown"],
        "payment_breakdown": overview["payment_breakdown"],
        "region_breakdown": overview["region_breakdown"],
        "cost_month_buckets": [
            {
                "bucket": month_bucket(index - 11),
                "monthly_cost": monthly_cost,
                "renewal_count": 0,
                "budget_limit": budget_months[index],
                "budget_currency": ASSET_WORKFLOW_BASE_CURRENCY if budget_months[index] is not None else None,
                "budget_warning_pct": 80 if budget_months[index] is not None else None,
                "data_insufficient": False,
            }
            for index, monthly_cost in enumerate(cost_months)
        ],
        "renewal_month_buckets": [
            {"bucket": month_bucket(0), "monthly_cost": 116.15, "renewal_count": 2, "data_insufficient": False},
            {"bucket": month_bucket(1), "monthly_cost": 58.4, "renewal_count": 1, "data_insufficient": False},
            {"bucket": month_bucket(2), "monthly_cost": 0, "renewal_count": 0, "data_insufficient": False},
        ],
        "budget_statuses": asset_workflow_budget_records(),
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

    currency = first_query_value(query, "currency")
    if currency:
        rows = [row for row in rows if row["currency"] == currency]

    budget_status = first_query_value(query, "budget_status")
    if budget_status:
        rows = [row for row in rows if row.get("budget_status") == budget_status]

    label = first_query_value(query, "label")
    if label:
        rows = [
            row
            for row in rows
            if label in [str(item) for item in row.get("labels", [])]
        ]

    provider_id = first_query_value(query, "provider_id")
    if provider_id:
        rows = [
            row
            for row in rows
            if asset_workflow_provider_label(row)[0] == provider_id
        ]

    renewal_decision = first_query_value(query, "renewal_decision")
    if renewal_decision:
        rows = [
            row
            for row in rows
            if next(
                vps["renewal_decision"]
                for vps in asset_workflow_vps_assets()
                if vps["vps_id"] == row["vps_id"]
            ) == renewal_decision
        ]

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


def request_json_payload(request: object) -> object:
    raw = getattr(request, "post_data", None)
    if raw is None:
        return {}
    if not raw:
        return {}
    return json.loads(raw)


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

    if method == "GET" and path == "/api/asset-decisions/overview":
        fulfill_json(route, 200, asset_decision_overview(query))
        return

    if method == "GET" and path == "/api/asset-decisions/groups":
        fulfill_json(route, 200, asset_decision_public_groups(query))
        return

    if path == "/api/asset-decisions/scenario-templates":
        if method == "GET":
            fulfill_json(route, 200, asset_decision_scenario_templates())
            return
        if method == "POST":
            detail = asset_decision_scenario_template_detail("adt_mock_manual_primary_standby")
            assert detail is not None
            created = dict(detail)
            payload = request_json_payload(request)
            created["template_id"] = "adt_mock_created"
            created["builtin"] = False
            if isinstance(payload, dict):
                for key in ["scenario", "title", "goal", "note", "source_manual_group_id"]:
                    if key in payload:
                        created[key] = payload[key]
            for member in created.get("members", []):
                if isinstance(member, dict):
                    member["template_id"] = "adt_mock_created"
            fulfill_json(route, 201, created)
            return

    if path.startswith("/api/asset-decisions/scenario-templates/"):
        parts = [part for part in path.split("/") if part]
        if len(parts) >= 4:
            template_id = parts[3]
            detail = asset_decision_scenario_template_detail(template_id)
            if detail is None and template_id == "adt_mock_created":
                detail = asset_decision_scenario_template_detail("adt_mock_manual_primary_standby")
            if detail is None:
                fulfill_json(route, 404, {"error": "asset decision scenario template not found"})
                return
            if len(parts) == 4:
                if method == "GET":
                    fulfill_json(route, 200, detail)
                    return
                if method == "PATCH":
                    patched = dict(detail)
                    payload = request_json_payload(request)
                    if isinstance(payload, dict) and not patched.get("builtin"):
                        for key in ["status", "title", "goal", "note"]:
                            if key in payload:
                                patched[key] = payload[key]
                    patched["updated_at"] = iso_timestamp(0)
                    if patched.get("status") == "archived":
                        patched["archived_at"] = iso_timestamp(0)
                    fulfill_json(route, 200, patched)
                    return
            if len(parts) == 5 and parts[4] == "manual-groups" and method == "POST":
                created = asset_decision_manual_group_detail("admg_mock_created")
                assert created is not None
                payload = request_json_payload(request)
                if isinstance(payload, dict):
                    for key in ["scenario", "title", "goal", "note", "status", "renew_within_days"]:
                        if key in payload:
                            created[key] = payload[key]
                fulfill_json(route, 201, created)
                return

    if path == "/api/asset-decisions/manual-groups":
        if method == "GET":
            fulfill_json(route, 200, asset_decision_manual_groups())
            return
        if method == "POST":
            detail = asset_decision_manual_group_detail("admg_mock_created")
            assert detail is not None
            fulfill_json(route, 201, detail)
            return

    if path.startswith("/api/asset-decisions/manual-groups/"):
        parts = [part for part in path.split("/") if part]
        if len(parts) >= 4:
            manual_group_id = parts[3]
            detail = asset_decision_manual_group_detail(manual_group_id)
            if detail is None:
                fulfill_json(route, 404, {"error": "asset decision manual group not found"})
                return
            if len(parts) == 4:
                if method == "GET":
                    fulfill_json(route, 200, detail)
                    return
                if method == "PATCH":
                    patched = dict(detail)
                    payload = request_json_payload(request)
                    if isinstance(payload, dict):
                        for key in ["status", "scenario", "title", "goal", "note"]:
                            if key in payload:
                                patched[key] = payload[key]
                    patched["updated_at"] = iso_timestamp(0)
                    fulfill_json(route, 200, patched)
                    return
            if len(parts) == 5 and parts[4] == "members" and method == "POST":
                patched = dict(detail)
                members = list(detail.get("members", []))
                vps_id = "vps_tokyo_lab"
                payload = request_json_payload(request)
                if isinstance(payload, dict) and isinstance(payload.get("vps_id"), str):
                    vps_id = str(payload["vps_id"])
                candidate = asset_decision_member(
                    vps_id,
                    suggested_role="evidence_needed",
                    suggested_action="complete_evidence",
                    evidence_chips=[
                        asset_decision_chip("missing_subscription", "缺订阅", "alert"),
                        asset_decision_chip("missing_monitoring", "未关联监控", "alert"),
                    ],
                )
                members.append(
                    asset_decision_manual_group_member(
                        candidate,
                        manual_group_id=manual_group_id,
                        intended_role="evidence_needed",
                        intended_action="complete_evidence",
                        reason="补齐证据后再纳入主备取舍。",
                        note="",
                        sort_order=30,
                    )
                )
                summary = asset_decision_manual_group_summary(manual_group_id, members)
                patched = {**summary, "members": members}
                fulfill_json(route, 201, patched)
                return
            if len(parts) == 6 and parts[4] == "members":
                if method == "PATCH":
                    patched = dict(detail)
                    payload = request_json_payload(request)
                    members = []
                    for member in detail.get("members", []):
                        if not isinstance(member, dict) or member.get("vps_id") != parts[5] or not isinstance(payload, dict):
                            members.append(member)
                            continue
                        next_member = dict(member)
                        for key in ["intended_role", "intended_action", "reason", "note", "sort_order"]:
                            if key in payload:
                                next_member[key] = payload[key]
                        next_member["updated_at"] = iso_timestamp(0)
                        members.append(next_member)
                    patched["members"] = members
                    patched["updated_at"] = iso_timestamp(0)
                    fulfill_json(route, 200, patched)
                    return
                if method == "DELETE":
                    patched = dict(detail)
                    patched["members"] = [
                        member
                        for member in detail.get("members", [])
                        if not isinstance(member, dict) or member.get("vps_id") != parts[5]
                    ]
                    patched["member_count"] = len(patched["members"])
                    patched["updated_at"] = iso_timestamp(0)
                    fulfill_json(route, 200, patched)
                    return

    if path == "/api/asset-decisions/records":
        if method == "GET":
            fulfill_json(route, 200, asset_decision_records())
            return
        if method == "POST":
            payload = request_json_payload(request)
            detail = asset_decision_record_detail()
            assert detail is not None
            created = dict(detail)
            created["record_id"] = "adr_mock_created"
            if isinstance(payload, dict) and payload.get("source_type") == "manual_group":
                created["source_type"] = "manual_group"
                created["source_group_id"] = str(payload.get("source_group_id") or "admg_mock_primary_standby")
                created["source_group_type"] = "region_portfolio"
                created["source_view"] = "region"
                created["scope_label"] = "用户场景 / 欧洲主备"
            for member in created["members"]:
                member["record_id"] = "adr_mock_created"
            fulfill_json(route, 201, created)
            return

    if path.startswith("/api/asset-decisions/records/"):
        parts = [part for part in path.split("/") if part]
        if len(parts) == 4:
            detail = asset_decision_record_detail(parts[3])
            if detail is not None:
                if method == "GET":
                    fulfill_json(route, 200, detail)
                    return
                if method == "PATCH":
                    patched = dict(detail)
                    payload = request_json_payload(request)
                    if isinstance(payload, dict) and payload.get("status"):
                        patched["status"] = payload["status"]
                    elif not isinstance(payload, dict) or not payload.get("members"):
                        patched["status"] = "completed"
                        patched["completed_at"] = iso_timestamp(0)
                    if isinstance(payload, dict):
                        for member_patch in payload.get("members", []):
                            if not isinstance(member_patch, dict):
                                continue
                            vps_id = member_patch.get("vps_id")
                            for member in patched.get("members", []):
                                if member.get("vps_id") != vps_id:
                                    continue
                                if "followup_status" in member_patch:
                                    member["followup_status"] = member_patch["followup_status"]
                                if "followup_note" in member_patch:
                                    member["followup_note"] = member_patch["followup_note"]
                                member["followup_updated_at"] = iso_timestamp(0)
                        statuses = [member.get("followup_status") for member in patched.get("members", [])]
                        patched["followup_todo_count"] = statuses.count("todo")
                        patched["followup_in_progress_count"] = statuses.count("in_progress")
                        patched["followup_blocked_count"] = statuses.count("blocked")
                        patched["followup_done_count"] = statuses.count("done")
                        patched["followup_skipped_count"] = statuses.count("skipped")
                    patched["updated_at"] = iso_timestamp(0)
                    fulfill_json(route, 200, patched)
                    return
            fulfill_json(route, 404, {"error": "asset decision record not found"})
            return

    if method == "GET" and path.startswith("/api/asset-decisions/groups/"):
        parts = [part for part in path.split("/") if part]
        if len(parts) == 4:
            detail = asset_decision_group_detail(parts[3])
            if detail is not None:
                fulfill_json(route, 200, detail)
                return
            fulfill_json(route, 404, {"error": "asset decision group not found"})
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
            if len(parts) == 4 and parts[3] == "archive-review":
                review = asset_workflow_archive_review(vps_id)
                if review is not None:
                    fulfill_json(route, 200, review)
                    return

    if method == "GET" and path == "/api/subscriptions":
        fulfill_json(route, 200, filter_asset_workflow_subscriptions(query))
        return

    if method == "GET" and path == "/api/subscriptions/overview":
        fulfill_json(route, 200, asset_workflow_subscription_overview())
        return

    if method == "GET" and path == "/api/subscriptions/statistics":
        fulfill_json(
            route,
            200,
            asset_workflow_subscription_statistics(first_query_value(query, "window")),
        )
        return

    if method == "GET" and path == "/api/subscriptions/settings":
        fulfill_json(route, 200, asset_workflow_subscription_settings())
        return

    if method == "GET" and path == "/api/subscription-budgets":
        fulfill_json(route, 200, asset_workflow_budget_records())
        return

    if method == "GET" and path == "/api/subscription-monthly-budgets":
        fulfill_json(route, 200, asset_workflow_monthly_budget_records())
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

                          function hasReliableBoxMetrics(el) {
                            return el instanceof HTMLElement;
                          }

                          const elements = Array.from(document.querySelectorAll('body *'));
                          const leafTextElements = elements.filter((el) => {
                            if (!hasVisibleText(el)) return false;
                            return !Array.from(el.children).some((child) => hasVisibleText(child));
                          });

                          const overflowingText = leafTextElements
                            .filter((el) => hasReliableBoxMetrics(el))
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
