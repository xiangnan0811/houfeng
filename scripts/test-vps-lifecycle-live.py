#!/usr/bin/env python3
"""Disposable real Center/Agent smoke. Never uses environment database credentials.

--keep leaves processes available for browser verification until STOP is created
inside the printed workspace. Secrets are written only to mode-0600 local files.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.request


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workspace", required=True)
    parser.add_argument("--keep", action="store_true")
    args = parser.parse_args()
    work = Path(args.workspace)
    os.umask(0o077)
    container = "houfeng-lifecycle-live-" + secrets.token_hex(6)
    pg_port, http_port = free_port(), free_port()
    origin = f"http://127.0.0.1:{http_port}"
    password = secrets.token_hex(24)
    user_password = secrets.token_hex(20)
    processes = []
    reports = []
    cookie = ""

    def report(name, **evidence):
        item = {"check": name, **evidence}
        reports.append(item)
        (work / "results.json").write_text(json.dumps(reports, ensure_ascii=False, indent=2))
        print(json.dumps(item, ensure_ascii=False), flush=True)

    def sql(query):
        result = subprocess.run(["docker", "exec", "-i", container, "psql", "-U", "postgres", "-d", "postgres", "-p", str(pg_port), "-At", "-v", "ON_ERROR_STOP=1"], input=query, text=True, capture_output=True)
        if result.returncode:
            raise RuntimeError("isolated database query failed: " + result.stderr[-1000:])
        return result.stdout.strip()

    def request(method, path, body=None, expected=200, headers=None):
        nonlocal cookie
        req_headers = {"Content-Type": "application/json", "Origin": origin}
        if cookie:
            req_headers["Cookie"] = cookie
        req_headers.update(headers or {})
        req = urllib.request.Request(origin + path, data=None if body is None else json.dumps(body).encode(), headers=req_headers, method=method)
        try:
            response = urllib.request.urlopen(req, timeout=15)
        except urllib.error.HTTPError as error:
            response = error
        payload = response.read()
        if response.status != expected:
            raise RuntimeError(f"{method} {path}: HTTP {response.status}, expected {expected}: {payload[:600].decode(errors='replace')}")
        if response.headers.get("Set-Cookie"):
            cookie = response.headers["Set-Cookie"].split(";", 1)[0]
        return json.loads(payload) if payload else None

    def wait_until(predicate, description, timeout=40):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(1)
        raise RuntimeError("timed out: " + description)

    def start_agent(label, token):
        token_file = work / f"{label}-token.json"
        token_file.write_text(token)
        env = {key: value for key, value in os.environ.items() if not key.startswith("HOUFENG_")}
        env.update(HOUFENG_AGENT_SERVER_URL=origin, HOUFENG_AGENT_TOKEN_FILE=str(token_file), HOUFENG_AGENT_BUFFER_FILE=str(work / f"{label}-queue.json"), HOUFENG_AGENT_IP_QUALITY_STATE_FILE=str(work / f"{label}-ip.json"))
        process = subprocess.Popen([str(work / "houfeng-agent")], env=env, stdout=(work / f"{label}.log").open("w"), stderr=subprocess.STDOUT)
        processes.append(process)
        return process, token_file

    try:
        started = subprocess.run(["docker", "run", "-d", "--name", container, "--network=host", "--tmpfs", "/var/lib/postgresql/data:rw,noexec,nosuid,size=1g", "-e", "POSTGRES_PASSWORD=" + password, "postgres:16-alpine", "-c", "port=" + str(pg_port)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if started.returncode:
            raise RuntimeError("could not start owned disposable PostgreSQL container")
        wait_until(lambda: subprocess.run(["docker", "exec", container, "pg_isready", "-U", "postgres", "-p", str(pg_port)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, "PostgreSQL ready")
        env = {key: value for key, value in os.environ.items() if not key.startswith("HOUFENG_")}
        env.update(HOUFENG_DATABASE_URL=f"postgres://postgres:{password}@127.0.0.1:{pg_port}/postgres?sslmode=disable", HOUFENG_HTTP_ADDR=f"127.0.0.1:{http_port}", HOUFENG_PUBLIC_BASE_URL=origin, HOUFENG_INITIAL_USERNAME="live-operator", HOUFENG_INITIAL_PASSWORD=user_password, HOUFENG_SESSION_HMAC_KEY=secrets.token_hex(32), HOUFENG_RECORDS_ENABLED="false", HOUFENG_RECORD_PERMANENT_DELETE_ENABLED="false", HOUFENG_WEB_DIST_DIR=str(Path.cwd() / "web/dist"))
        center = subprocess.Popen([str(work / "houfeng-center")], env=env, stdout=(work / "center.log").open("w"), stderr=subprocess.STDOUT)
        processes.append(center)
        def ready():
            if center.poll() is not None:
                raise RuntimeError("Center stopped during startup; inspect center.log")
            try:
                urllib.request.urlopen(origin + "/healthz", timeout=1)
                return True
            except urllib.error.HTTPError:
                return True
            except OSError:
                return False
        wait_until(ready, "Center ready", 90)
        request("POST", "/api/auth/login", {"username": "live-operator", "password": user_password})
        (work / "browser-access.json").write_text(json.dumps({"origin": origin, "username": "live-operator", "password": user_password, "container": container, "pg_port": pg_port, "center_pid": center.pid}))
        report("fresh_center", origin=origin, records_enabled=False, evidence="matching local binaries, disposable PostgreSQL")
        vps = request("POST", "/api/vps", {"display_name": "真实 Agent 生命周期验收", "provider_name": "Local fixture", "region": "Singapore", "city": "Singapore", "usage_tags": ["验收"], "validity_mode": "unlimited"}, 201, {"Idempotency-Key": secrets.token_hex(16)})
        vps_id = vps["vps_id"]
        overview = request("GET", f"/api/vps/{vps_id}/overview")
        assert overview["identity"]["vps_id"] == vps_id
        report("core_vps_overview", status=200, correct_identity=True, records_enabled=False)
        created = request("POST", f"/api/vps/{vps_id}/monitoring-instances", {}, 201, {"Idempotency-Key": secrets.token_hex(16)})
        mi = created["monitoring_instance_id"]
        issue = request("POST", f"/api/monitoring-instances/{mi}/enrollment-token", {})
        agent, token_file = start_agent("agent-original", issue["token"])
        wait_until(lambda: sql(f"select ever_connected from monitoring_instances where monitoring_instance_id='{mi}'") == "t", "first actual Agent trusted heartbeat")
        wait_until(lambda: int(sql(f"select count(*) from host_samples where monitoring_instance_id='{mi}'")) > 0, "first actual performance sample")
        report("agent_first_enrollment", vps_id=vps_id, monitoring_instance_id=mi, lifecycle=request("GET", f"/api/monitoring-instances/{mi}")["lifecycle_status"])
        request("POST", f"/api/monitoring-instances/{mi}/runtime/pause", {})
        time.sleep(7)
        before = sql(f"select count(*) from host_samples where monitoring_instance_id='{mi}'")
        live_before = sql(f"select last_trusted_online_at from monitoring_instances where monitoring_instance_id='{mi}'")
        time.sleep(7)
        assert sql(f"select count(*) from host_samples where monitoring_instance_id='{mi}'") == before
        assert sql(f"select last_trusted_online_at from monitoring_instances where monitoring_instance_id='{mi}'") != live_before
        review = request("GET", f"/api/vps/{vps_id}/archive-review")
        assert not review["eligible"]
        report("pause", performance_stopped=True, live_signal_continues=True, archive_blocked=True)
        retire_body = {"reason": "真实 Agent 退役验收"}
        retire_key = secrets.token_hex(16)
        request("POST", f"/api/monitoring-instances/{mi}/lifecycle/retire", retire_body, headers={"Idempotency-Key": retire_key})
        request("POST", f"/api/monitoring-instances/{mi}/lifecycle/retire", retire_body, headers={"Idempotency-Key": retire_key})
        request("POST", f"/api/monitoring-instances/{mi}/lifecycle/retire", retire_body, 409, {"Idempotency-Key": secrets.token_hex(16)})
        live_before = sql(f"select last_trusted_online_at from monitoring_instances where monitoring_instance_id='{mi}'")
        time.sleep(6)
        assert sql(f"select last_trusted_online_at from monitoring_instances where monitoring_instance_id='{mi}'") != live_before
        assert all(item["monitoring_instance_id"] != mi for item in request("GET", "/api/monitoring-instances"))
        report("retire", duplicate_rejected=True, retry_idempotent=True, default_list_removed=True, old_agent_still_identified=True)
        request("POST", f"/api/monitoring-instances/{mi}/binding/reset", {})
        issue = request("POST", f"/api/monitoring-instances/{mi}/enrollment-token", {})
        new_agent, new_token_file = start_agent("agent-reenrolled", issue["token"])
        wait_until(lambda: sql(f"select count(*) from monitoring_agent_sessions where monitoring_instance_id='{mi}'") == "2", "new enrollment session")
        wait_until(lambda: sql(f"select count(*) from monitoring_agent_sessions where monitoring_instance_id='{mi}' and capability='full' and ever_connected") == "1", "new session online")
        assert json.loads(token_file.read_text())["sync_token"] != json.loads(new_token_file.read_text())["sync_token"]
        assert sql(f"select count(*) from monitoring_agent_sessions where monitoring_instance_id='{mi}' and capability='evidence_only'") == "1"
        report("reenrollment", same_instance=True, new_session=True, old_session_evidence_only=True)
        archive_vps = request("POST", "/api/vps", {"display_name": "从未接入与共享关系验收", "usage_tags": ["临时用途"]}, 201, {"Idempotency-Key": secrets.token_hex(16)})
        archive_id = archive_vps["vps_id"]
        service = request("POST", "/api/services", {"vps_id": archive_id, "name": "跨 VPS 共享服务", "service_type": "web"}, 201, {"Idempotency-Key": secrets.token_hex(16)})
        request("POST", f"/api/vps/{vps_id}/service-associations", {"object_id": service["service_id"], "address": "127.0.0.1", "port": 8080}, 201)
        archive_review = request("GET", f"/api/vps/{archive_id}/archive-review")
        assert archive_review["online_evidence"]["never_connected"]
        archive_body = {"confirmation_name": archive_vps["display_name"], "reason": "从未接入人工确认测试", "preview_digest": archive_review["preview_digest"], "idempotency_key": secrets.token_hex(16), "never_connected_confirmation": True}
        archived = request("POST", f"/api/vps/{archive_id}/archive", archive_body)
        replay = request("POST", f"/api/vps/{archive_id}/archive", archive_body)
        assert archived == replay
        assert request("GET", f"/api/vps/{archive_id}")["lifecycle_status"] == "archived"
        assert request("GET", f"/api/vps/{archive_id}/service-associations?current=true") == []
        assert len(request("GET", f"/api/vps/{vps_id}/service-associations?current=true")) == 1
        assert sql(f"select status from asset_services where service_id='{service['service_id']}'") == "active"
        request("POST", f"/api/vps/{archive_id}/restore-from-archive", {"reason": "恢复验收"})
        restored = request("GET", f"/api/vps/{archive_id}")
        assert restored["lifecycle_status"] == "active" and restored["usage_tags"] == ["闲置"]
        assert request("GET", f"/api/vps/{archive_id}/service-associations?current=true") == []
        assert request("GET", f"/api/vps/{archive_id}/services") == []
        assert request("GET", f"/api/vps/{archive_id}/domains") == []
        restored_overview = request("GET", f"/api/vps/{archive_id}/overview")
        relation_counts = {item["kind"]: item["count"] for item in restored_overview["relations"]}
        assert relation_counts["services"] == 0 and relation_counts["domains"] == 0
        historic_associations = request("GET", f"/api/vps/{archive_id}/service-associations")
        assert len(historic_associations) == 1
        assert historic_associations[0]["object_id"] == service["service_id"] and historic_associations[0]["ended_at"]
        report("restored_relationship_projection", current_services=0, current_domains=0, overview_services=relation_counts["services"], overview_domains=relation_counts["domains"], ended_service_association_preserved=True)
        report("never_connected_archive_restore", manual_exception=True, archive_retry_idempotent=True, shared_service_preserved=True, ended_association_not_reopened=True, restored_usage_tags=restored["usage_tags"])

        # Only this scenario accelerates time through direct SQL in this owned
        # disposable database. It is evidence of atomic/archive reappearance
        # behavior, never evidence that 180 minutes elapsed in a real deployment.
        agent.terminate(); agent.wait(timeout=10)
        new_agent.terminate(); new_agent.wait(timeout=10)
        old_credential = json.loads(token_file.read_text())["sync_token"]
        old_session_id = old_credential.split(".", 1)[0]
        old_signal = json.loads(sql(f"select json_build_object('id',l.signal_id,'fingerprint',s.fingerprint_hash) from agent_live_signals l join monitoring_agent_sessions s using(session_id) where l.session_id='{old_session_id}' order by l.received_at limit 1"))
        trusted_before_replay = sql(f"select last_trusted_online_at from monitoring_instances where monitoring_instance_id='{mi}'")
        replay_result = request("POST", "/api/agent/sync", {"monitoring_instance_id": mi, "session_id": old_session_id, "live_signal": old_signal}, headers={"Authorization": "Bearer " + old_credential})
        assert replay_result["stop_collection"]
        assert sql(f"select last_trusted_online_at from monitoring_instances where monitoring_instance_id='{mi}'") == trusted_before_replay
        report("online_signal_replay", accepted_old_session=True, no_trusted_time_refresh=True, collection_denied=True)
        sql(f"update monitoring_agent_sessions set started_at=clock_timestamp()-interval '1 day',last_trusted_online_at=clock_timestamp()-interval '181 minutes' where monitoring_instance_id='{mi}'; update monitoring_instances set last_trusted_online_at=clock_timestamp()-interval '181 minutes' where monitoring_instance_id='{mi}'; update receiver_health set healthy_since=clock_timestamp()-interval '181 minutes',checked_at=clock_timestamp(),healthy=true")
        archive_review = request("GET", f"/api/vps/{vps_id}/archive-review")
        assert archive_review["eligible"], archive_review["blockers"]
        request("POST", f"/api/vps/{vps_id}/archive", {"confirmation_name": vps["display_name"], "reason": "受控时钟夹具归档验收", "preview_digest": archive_review["preview_digest"], "idempotency_key": secrets.token_hex(16)})
        start_agent("agent-after-archive", token_file.read_text())
        wait_until(lambda: sql(f"select count(*) from vps_followups where vps_id='{vps_id}' and kind='archived_online'") == "1", "archived actual Agent reappearance followup")
        time.sleep(6)
        assert sql(f"select count(*) from vps_followups where vps_id='{vps_id}' and kind='archived_online'") == "1"
        assert request("GET", f"/api/vps/{vps_id}")["lifecycle_status"] == "archived"
        report("archived_agent_reappeared", actual_agent=True, safety_timestamps="controlled SQL fixture aged 181 minutes, not elapsed-time acceptance", archived_state_preserved=True, followup_deduplicated=True)
        report("api_smoke_ready", workspace=str(work), browser_credentials_file=str(work / "browser-access.json"))
    except Exception as error:
        report("failure", error=str(error), workspace=str(work))
        raise
    finally:
        if args.keep:
            print("Awaiting browser completion: create " + str(work / "STOP"), flush=True)
            while not (work / "STOP").exists():
                time.sleep(1)
        for process in reversed(processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        subprocess.run(["docker", "rm", "-f", container], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
