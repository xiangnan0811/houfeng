import importlib.util
import json
import sys
import unittest
from pathlib import Path

SCRIPT_PATH = Path(__file__).with_name("visual_evidence.py")
SPEC = importlib.util.spec_from_file_location("visual_evidence", SCRIPT_PATH)
assert SPEC is not None and SPEC.loader is not None
visual_evidence = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = visual_evidence
SPEC.loader.exec_module(visual_evidence)


class FakeRequest:
    def __init__(self, url: str, method: str = "GET") -> None:
        self.url = url
        self.method = method


class FakeRoute:
    def __init__(self, path: str, query: str = "", method: str = "GET") -> None:
        suffix = f"?{query}" if query else ""
        self.request = FakeRequest(f"http://127.0.0.1:5178{path}{suffix}", method)
        self.status: int | None = None
        self.content_type: str | None = None
        self.body: str | None = None

    def fulfill(self, status: int, content_type: str, body: str) -> None:
        self.status = status
        self.content_type = content_type
        self.body = body


def call_observability_api(path: str, query: str = "") -> tuple[int, object]:
    route = FakeRoute(path, query)
    visual_evidence.fulfill_observability_support_api(route)
    assert route.status is not None
    assert route.body is not None
    return route.status, json.loads(route.body)


def call_asset_workflow_api(path: str, query: str = "", method: str = "GET") -> tuple[int, object]:
    route = FakeRoute(path, query, method)
    visual_evidence.fulfill_asset_workflow_api(route)
    assert route.status is not None
    assert route.body is not None
    return route.status, json.loads(route.body)


class VisualEvidenceMockAPITest(unittest.TestCase):
    def test_argparse_accepts_observability_support_profile(self) -> None:
        parser = visual_evidence.build_parser()
        args = parser.parse_args(
            [
                "browser-sanity",
                "--base-url",
                "http://127.0.0.1:5178/",
                "--mock-api",
                "observability-support",
                "--route",
                "/monitoring",
            ]
        )
        self.assertEqual(args.mock_api, "observability-support")

    def test_observability_profile_serves_auth_dashboard_nodes_targets(self) -> None:
        status, user = call_observability_api("/api/auth/me")
        self.assertEqual(status, 200)
        self.assertEqual(user["username"], "observability-evidence")

        status, settings = call_observability_api("/api/settings")
        self.assertEqual(status, 200)
        self.assertEqual(settings["incident_defaults"]["load5_critical"], 8.0)
        self.assertEqual(settings["ip_quality_settings"]["stale_after_seconds"], 604800)

        status, dashboard = call_observability_api("/api/dashboard")
        self.assertEqual(status, 200)
        self.assertGreaterEqual(dashboard["abnormal_monitoring_instance_count"], 2)
        self.assertGreaterEqual(dashboard["abnormal_target_count"], 2)

        status, monitoring = call_observability_api("/api/monitoring-instances")
        self.assertEqual(status, 200)
        monitoring_instance_ids = {monitoring_instance["monitoring_instance_id"] for monitoring_instance in monitoring}
        self.assertIn("mi_hkg_edge_01", monitoring_instance_ids)
        self.assertIn("mi_pending_sfo_02", monitoring_instance_ids)
        self.assertIn("mi_ams_conflict_03", monitoring_instance_ids)
        self.assertIn("mi_fra_maint_04", monitoring_instance_ids)

        status, targets = call_observability_api("/api/targets")
        self.assertEqual(status, 200)
        target_ids = {target["target_id"] for target in targets}
        self.assertIn("target_api_core", target_ids)
        self.assertIn("target_docs_paused", target_ids)
        self.assertIn("target_legacy_archived", target_ids)

    def test_observability_profile_serves_sparklines(self) -> None:
        status, monitoring_instance_sparklines = call_observability_api(
            "/api/monitoring-instances/sparklines",
            "metrics=cpu_usage_pct,mem_used_pct,disk_used_pct",
        )
        self.assertEqual(status, 200)
        self.assertIn("mi_hkg_edge_01", monitoring_instance_sparklines["monitoring_instances"])
        self.assertIn(
            "cpu_usage_pct",
            monitoring_instance_sparklines["monitoring_instances"]["mi_hkg_edge_01"],
        )

        status, target_sparklines = call_observability_api("/api/targets/sparklines")
        self.assertEqual(status, 200)
        self.assertIn("target_api_core", target_sparklines["targets"])
        self.assertGreater(
            len(target_sparklines["targets"]["target_api_core"]["latency"]),
            0,
        )

    def test_observability_events_filtering_and_backfilled_opt_in(self) -> None:
        status, default_events = call_observability_api("/api/events", "limit=100")
        self.assertEqual(status, 200)
        default_ids = {event["event_id"] for event in default_events["items"]}
        self.assertIn("event_monitoring_instance_severe_started", default_ids)
        self.assertNotIn("event_backfilled_monitoring_instance", default_ids)

        status, with_backfilled = call_observability_api(
            "/api/events",
            "limit=100&include_backfilled=true",
        )
        self.assertEqual(status, 200)
        with_backfilled_ids = {event["event_id"] for event in with_backfilled["items"]}
        self.assertIn("event_backfilled_monitoring_instance", with_backfilled_ids)

        status, severe = call_observability_api("/api/events", "severity=%E4%B8%A5%E9%87%8D")
        self.assertEqual(status, 200)
        self.assertTrue(severe["items"])
        self.assertTrue(all(event["severity"] == "严重" for event in severe["items"]))

        status, maintenance = call_observability_api("/api/events", "maintenance_only=true")
        self.assertEqual(status, 200)
        self.assertTrue(maintenance["items"])
        self.assertTrue(
            all("maintenance" in event["event_type"] for event in maintenance["items"])
        )

        status, recovery = call_observability_api("/api/events", "recovery_only=true")
        self.assertEqual(status, 200)
        self.assertTrue(recovery["items"])
        self.assertTrue(
            all(event["event_type"] == "incident_recovered" for event in recovery["items"])
        )

        status, notification = call_observability_api("/api/events", "notification_only=true")
        self.assertEqual(status, 200)
        self.assertTrue(notification["items"])
        notification_ids = {event["event_id"] for event in notification["items"]}
        self.assertIn("event_target_china_notice", notification_ids)

    def test_observability_profile_returns_profile_specific_404_for_unknown_route(self) -> None:
        status, body = call_observability_api("/api/vps")
        self.assertEqual(status, 404)
        self.assertEqual(
            body["error"],
            "mock observability support API has no fixture for this request",
        )
        self.assertEqual(body["path"], "/api/vps")

    def test_asset_workflows_profile_still_serves_asset_routes(self) -> None:
        status, overview = call_asset_workflow_api(
            "/api/asset-decisions/overview",
            "view=needs_decision&renew_within_days=30",
        )
        self.assertEqual(status, 200)
        self.assertGreaterEqual(overview["group_count"], 5)
        self.assertTrue(overview["source_availability"]["subscriptions"])

        status, provider_groups = call_asset_workflow_api(
            "/api/asset-decisions/groups",
            "view=provider&renew_within_days=30",
        )
        self.assertEqual(status, 200)
        self.assertTrue(provider_groups)
        self.assertTrue(all(group["view"] == "provider" for group in provider_groups))
        self.assertIn("comparison_insight", provider_groups[0])
        self.assertTrue(provider_groups[0]["comparison_insight"]["lane_counts"])

        group_id = provider_groups[0]["group_id"]
        status, group_detail = call_asset_workflow_api(f"/api/asset-decisions/groups/{group_id}")
        self.assertEqual(status, 200)
        self.assertEqual(group_detail["group_id"], group_id)
        self.assertTrue(group_detail["members"])
        self.assertIn("comparison_insight", group_detail["members"][0])

        status, records = call_asset_workflow_api("/api/asset-decisions/records")
        self.assertEqual(status, 200)
        self.assertTrue(records)
        self.assertEqual(records[0]["record_id"], "adr_mock_eu_renewal")
        self.assertEqual(records[0]["source_type"], "auto_group")
        self.assertIn("comparison_insight", records[0]["evidence_snapshot"])

        status, record_detail = call_asset_workflow_api("/api/asset-decisions/records/adr_mock_eu_renewal")
        self.assertEqual(status, 200)
        self.assertEqual(record_detail["record_id"], "adr_mock_eu_renewal")
        self.assertEqual(len(record_detail["members"]), 3)
        self.assertEqual(record_detail["members"][0]["decided_action"], "keep")
        self.assertIn("comparison_insight", record_detail["members"][0]["evidence_snapshot"])

        status, created_record = call_asset_workflow_api("/api/asset-decisions/records", method="POST")
        self.assertEqual(status, 201)
        self.assertEqual(created_record["record_id"], "adr_mock_created")

        status, patched_record = call_asset_workflow_api("/api/asset-decisions/records/adr_mock_eu_renewal", method="PATCH")
        self.assertEqual(status, 200)
        self.assertEqual(patched_record["status"], "completed")

        status, missing_record = call_asset_workflow_api("/api/asset-decisions/records/adr_missing")
        self.assertEqual(status, 404)
        self.assertEqual(missing_record["error"], "asset decision record not found")

        status, missing_group = call_asset_workflow_api("/api/asset-decisions/groups/adg_auto_missing")
        self.assertEqual(status, 404)
        self.assertEqual(missing_group["error"], "asset decision group not found")

        status, assets = call_asset_workflow_api("/api/vps", "renewal_decision=unreviewed")
        self.assertEqual(status, 200)
        self.assertTrue(assets)
        self.assertTrue(all(asset["renewal_decision"] == "unreviewed" for asset in assets))

        status, detail = call_asset_workflow_api("/api/vps/vps_fra_legacy")
        self.assertEqual(status, 200)
        self.assertEqual(detail["vps_id"], "vps_fra_legacy")
        self.assertTrue(detail["monitoring_instance_links"])

        status, preview = call_asset_workflow_api("/api/vps/vps_fra_legacy/cancellation-preview")
        self.assertEqual(status, 200)
        self.assertEqual(preview["vps"]["vps_id"], "vps_fra_legacy")
        self.assertTrue(preview["target_links"])

        status, archive_review = call_asset_workflow_api("/api/vps/vps_archive_old/archive-review")
        self.assertEqual(status, 200)
        self.assertEqual(archive_review["vps"]["vps_id"], "vps_archive_old")
        self.assertEqual(archive_review["vps"]["lifecycle_status"], "archived")
        self.assertFalse(archive_review["eligible"])
        self.assertTrue(archive_review["subscriptions"])
        self.assertIn("monitoring_instance_links", archive_review)
        self.assertIn("target_links", archive_review)

        status, monitoring = call_asset_workflow_api("/api/monitoring-instances")
        self.assertEqual(status, 200)
        self.assertIn("mi_hkg_edge_01", {monitoring_instance["monitoring_instance_id"] for monitoring_instance in monitoring})

        status, targets = call_asset_workflow_api("/api/targets")
        self.assertEqual(status, 200)
        self.assertIn("target_api_core", {target["target_id"] for target in targets})

        status, statistics = call_asset_workflow_api("/api/subscriptions/statistics", "window=year")
        self.assertEqual(status, 200)
        self.assertEqual(statistics["window"], "year")
        self.assertEqual(len(statistics["cost_month_buckets"]), 12)
        self.assertTrue(
            any(bucket["monthly_cost"] > 0 for bucket in statistics["cost_month_buckets"])
        )

        status, settings = call_asset_workflow_api("/api/settings")
        self.assertEqual(status, 200)
        self.assertEqual(settings["incident_defaults"]["disk_alert_pct"], 92)
        self.assertEqual(settings["ip_quality_settings"]["stale_after_seconds"], 604800)
        self.assertEqual(settings["subscription_cost_settings"]["base_currency"], "CNY")


if __name__ == "__main__":
    unittest.main()
