"""Owned actual service lifecycle for the PostgreSQL bootstrap helper."""
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import struct
import subprocess
import sys
import threading
import re
import time
import urllib.request

IMAGE = "linnea7171/houfeng:v1.19.0"
CLAM = "clamav/clamav:1.4.3"
HERE = Path(__file__).resolve().parent
CLAMDB = HERE.parents[2] / "tmp/agent/g2arc-5u6fzq04/main-http-smoke/clamdb"

class RuntimeFailure(Exception):
    pass

def invoke(args, timeout=60):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if result.returncode:
        raise RuntimeFailure("command failed: " + args[0] + " status=" + str(result.returncode))
    return result.stdout

def save_private(path, body):
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as file: file.write(body)

def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]

class TLSForwarder:
    def __init__(self, target, certificate, key):
        self.target = target
        self.context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        self.context.load_cert_chain(certificate, key)
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen(32)
        self.listener.settimeout(0.5)
        self.port = self.listener.getsockname()[1]
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()
    def run(self):
        while not self.stop.is_set():
            try: conn, _ = self.listener.accept()
            except socket.timeout: continue
            except OSError: return
            threading.Thread(target=self.relay, args=(conn,), daemon=True).start()
    def relay(self, conn):
        upstream = None
        try:
            conn = self.context.wrap_socket(conn, server_side=True)
            upstream = socket.create_connection(("127.0.0.1", self.target), timeout=5)
            upstream.settimeout(None)
            def pipe(a, b):
                try:
                    while True:
                        chunk = a.recv(65536)
                        if not chunk: break
                        b.sendall(chunk)
                except OSError: pass
                finally:
                    try: b.shutdown(socket.SHUT_WR)
                    except OSError: pass
            sender = threading.Thread(target=pipe, args=(conn, upstream), daemon=True)
            sender.start()
            pipe(upstream, conn)
            sender.join(timeout=5)
        except (OSError, ssl.SSLError): pass
        finally:
            if upstream: upstream.close()
            conn.close()
    def close(self):
        self.stop.set()
        self.listener.close()
        self.thread.join(timeout=2)

def clamd_scan(scanner_port, data):
    with socket.create_connection(("127.0.0.1", scanner_port), timeout=10) as sock:
        sock.settimeout(30)
        sock.sendall(b"zINSTREAM\0" + struct.pack("!I", len(data)) + data + b"\0\0\0\0")
        out = b""
        while not out.endswith(b"\0") and len(out) < 4096:
            chunk = sock.recv(4096)
            if not chunk: break
            out += chunk
        return out

def run(state, binaries, run_dir, db_port, runtime_password, worker_password, measurements):
    if not CLAMDB.is_dir() or any(not (CLAMDB / name).is_file() for name in ("main.cvd", "daily.cvd", "bytecode.cvd")):
        raise RuntimeFailure("historical clamdb or required signature file is missing")
    names = []
    forwarder = None
    ephemeral_files = []
    events = []
    redact = [runtime_password, worker_password]
    deletion_private = run_dir / "deletion-private"
    completed = False
    def name(label):
        value = "g2arc-" + label + "-" + secrets.token_hex(8)
        names.append(value)
        return value
    def mounted(src, dst, ro=True):
        return "type=bind,src=" + str(src) + ",dst=" + dst + (",readonly" if ro else "")
    def alive(container):
        return invoke(["docker", "inspect", "--format", "{{.State.Running}}", container]).strip() == b"true"
    def start_runtime(label, entry, envfile, mounts, uid, extra=()):
        n = name(label)
        cmd = ["docker", "run", "-d", "--name", n, "--network=host", "--add-host", "db:127.0.0.1", "--user", uid, "--cap-drop=ALL", "--security-opt", "no-new-privileges"]
        if uid == "10002:10002": cmd += ["--group-add", "10001"]
        if envfile: cmd += ["--env-file", str(envfile)]
        for src, dst, ro in mounts: cmd += ["--mount", mounted(src, dst, ro)]
        cmd += list(extra) + ["--entrypoint", entry, IMAGE]
        if label == "authority": cmd += ["record-authority", "--scope", "compose"]
        invoke(cmd)
        events.append({"stage": "start", "process": label, "uid": uid, "executable": entry})
        return n
    def await_tcp(container, address, timeout=120):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if not alive(container): raise RuntimeFailure("process exited before listening: " + container)
            try:
                with socket.create_connection(address, timeout=1): return
            except OSError: time.sleep(0.25)
        raise RuntimeFailure("service listen deadline exceeded: " + container)
    def await_authority_health(container, timeout=120):
        started = time.monotonic()
        observations = {}
        while time.monotonic() - started < timeout:
            if not alive(container):
                events.append({"stage": "authority-health-failed", "reason": "process-exited", "observations": observations})
                raise RuntimeFailure("authority exited before health readiness")
            conn = http.client.HTTPConnection("127.0.0.1", 16002, timeout=2)
            try:
                conn.request("GET", "/healthz")
                response = conn.getresponse()
                status = response.status
                response.read()
                key = "http_" + str(status)
                observations[key] = observations.get(key, 0) + 1
                if status == 204:
                    events.append({"stage": "authority-health", "http_status": status, "elapsed_ms": int((time.monotonic() - started) * 1000), "observations": observations})
                    return
                if status != 503:
                    events.append({"stage": "authority-health-failed", "reason": "unexpected-status", "http_status": status, "observations": observations})
                    raise RuntimeFailure("authority health returned unexpected HTTP status " + str(status))
            except (OSError, http.client.HTTPException) as exc:
                key = type(exc).__name__
                observations[key] = observations.get(key, 0) + 1
            finally:
                conn.close()
            time.sleep(0.25)
        events.append({"stage": "authority-health-failed", "reason": "deadline", "observations": observations})
        raise RuntimeFailure("authority health readiness deadline exceeded")

    def await_clamd(container, scanner_port, timeout=360):
        started = time.monotonic()
        failures = {}
        while time.monotonic() - started < timeout:
            if not alive(container):
                events.append({"stage": "scanner-readiness-failed", "reason": "process-exited", "transient_errors": failures})
                raise RuntimeFailure("clamd exited before protocol readiness")
            try:
                with socket.create_connection(("127.0.0.1", scanner_port), timeout=2) as sock:
                    sock.settimeout(2)
                    sock.sendall(b"zPING\0")
                    response = b""
                    while len(response) < 5:
                        part = sock.recv(5 - len(response))
                        if not part: raise ConnectionResetError("clamd closed readiness connection")
                        response += part
                if response == b"PONG\0":
                    events.append({"stage": "scanner-readiness", "protocol": "PING/PONG", "elapsed_ms": int((time.monotonic() - started) * 1000), "transient_errors": failures})
                    return
                raise RuntimeFailure("clamd returned an unexpected PING response")
            except OSError as exc:
                kind = type(exc).__name__
                failures[kind] = failures.get(kind, 0) + 1
                time.sleep(0.25)
        events.append({"stage": "scanner-readiness-failed", "reason": "deadline", "transient_errors": failures})
        raise RuntimeFailure("clamd protocol readiness deadline exceeded")

    def stop(container, label):
        invoke(["docker", "stop", "--time", "30", container], timeout=40)
        inspect = json.loads(invoke(["docker", "inspect", "--format", "{{json .State}}", container]))
        events.append({"stage": "stop", "process": label, "exit_code": inspect["ExitCode"], "started_at": inspect["StartedAt"], "finished_at": inspect["FinishedAt"]})
        if inspect["ExitCode"] != 0: raise RuntimeFailure(label + " did not shut down successfully")
    try:
        certificate, key = run_dir / "tls.crt", run_dir / "tls.key"
        ephemeral_files.extend([certificate, key])
        invoke(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1", "-keyout", str(key), "-out", str(certificate)], timeout=30)
        os.chmod(key, 0o600)
        center_port = port()
        forwarder = TLSForwarder(center_port, str(certificate), str(key))
        base = "https://127.0.0.1:" + str(forwarder.port)
        scanner = name("clamd")
        invoke(["docker", "run", "-d", "--name", scanner, "-p", "127.0.0.1::3310", "--mount", mounted(CLAMDB, "/var/lib/clamav"), "--entrypoint", "/usr/sbin/clamd", CLAM, "--foreground"])
        mapping = invoke(["docker", "port", scanner, "3310/tcp"]).decode().strip()
        if not mapping.startswith("127.0.0.1:"): raise RuntimeFailure("scanner port is not loopback bound")
        scanner_port = int(mapping.rsplit(":", 1)[1])
        events.append({"stage": "scanner-start", "container": scanner, "loopback_port": scanner_port})
        await_clamd(scanner, scanner_port)
        events.append({"stage": "scanner-clean-scan-start"})
        clean = clamd_scan(scanner_port, b"isolated clean content")
        eicar = b"X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
        events.append({"stage": "scanner-eicar-scan-start"})
        infected = clamd_scan(scanner_port, eicar)
        if clean != b"stream: OK\0" or b"Eicar-Test-Signature FOUND\0" not in infected:
            raise RuntimeFailure("real clamd did not classify clean and EICAR streams")
        scanner_hashes = {}
        for filename in ("main.cvd", "daily.cvd", "bytecode.cvd"):
            digest = hashlib.sha256()
            with (CLAMDB / filename).open("rb") as f:
                for block in iter(lambda: f.read(1024 * 1024), b""): digest.update(block)
            scanner_hashes[filename] = digest.hexdigest()
        events.append({"stage": "scanner", "clean": "OK", "eicar": "FOUND", "image": CLAM, "image_id": invoke(["docker", "image", "inspect", "--format", "{{.Id}}", CLAM]).decode().strip(), "signature_sha256": scanner_hashes})
        common = {"HOUFENG_DATABASE_URL": "postgres://houfeng_runtime:" + runtime_password + "@127.0.0.1:" + str(db_port) + "/houfeng?sslmode=disable", "HOUFENG_ATTACHMENT_BLOB_BACKEND": "local", "HOUFENG_ATTACHMENT_BLOB_ROOT": "/var/lib/houfeng/attachments", "HOUFENG_CLAMAV_NETWORK": "tcp", "HOUFENG_CLAMAV_ADDRESS": "127.0.0.1:" + str(scanner_port), "HOUFENG_RECORD_DEPLOYMENT_ID_FILE": "/run/houfeng/records-authority/deployment-id"}
        password = secrets.token_hex(24)
        center_env = dict(common, HOUFENG_HTTP_ADDR="127.0.0.1:" + str(center_port), HOUFENG_PUBLIC_BASE_URL=base, HOUFENG_REQUIRE_HTTPS_PUBLIC_BASE_URL="true", HOUFENG_INITIAL_USERNAME="smokeadmin", HOUFENG_INITIAL_PASSWORD=password, HOUFENG_SESSION_HMAC_KEY=secrets.token_hex(32), HOUFENG_RECORDS_ENABLED="true", HOUFENG_RECORD_PERMANENT_DELETE_ENABLED="false", HOUFENG_RECORD_INSTANCE_ID="compose-center", HOUFENG_RECORD_INSTANCE_KIND="api", HOUFENG_RECORD_INSTANCE_CAPABILITY="records.runtime", HOUFENG_PORTABILITY_ENABLED="true", HOUFENG_RECORD_BIRTH_LAUNCH_FILE="/run/houfeng/records-authority/birth-launch.json")
        redact.extend([password, center_env["HOUFENG_SESSION_HMAC_KEY"]])
        processor_env = dict(common, HOUFENG_RECORD_INSTANCE_ID="compose-center", HOUFENG_RECORD_INSTANCE_KIND="api", HOUFENG_RECORD_INSTANCE_CAPABILITY="records.runtime", HOUFENG_RECORD_BIRTH_LAUNCH_FILE="/run/houfeng/records-authority/birth-launch-processor.json", HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT="/var/lib/houfeng/processor-workspace", HOUFENG_CONTENT_PROCESSOR_OWNER_ID="compose-processor")
        env_center, env_processor, credential = run_dir / "center.env", run_dir / "processor.env", run_dir / "http-credentials.json"
        ephemeral_files += [env_center, env_processor, credential]
        save_private(env_center, "".join(k + "=" + v + "\n" for k, v in center_env.items()))
        save_private(env_processor, "".join(k + "=" + v + "\n" for k, v in processor_env.items()))
        save_private(credential, json.dumps({"username": "smokeadmin", "password": password}))
        observer_env = run_dir / "observer.env"
        ephemeral_files.append(observer_env)
        save_private(observer_env, "PGPASSWORD=" + runtime_password + "\n")
        binary_mounts = [(binaries / binary, "/usr/local/bin/" + binary, True) for binary in ("houfeng-center", "houfeng-content-processor", "houfeng-record-platform-admin", "deletion-smoke")]
        # The fixed authority health port must be unoccupied before launch.
        with socket.socket() as check: check.bind(("127.0.0.1", 16002))
        authority = start_runtime("authority", "/usr/local/bin/houfeng-record-platform-admin", None, [(state, "/var/lib/houfeng", True), (state / "maintenance", "/var/lib/houfeng-maintenance", False)] + binary_mounts, "10002:10002")
        await_authority_health(authority)
        app_mounts = [(state / "attachments", "/var/lib/houfeng/attachments", False), (state / "center-config", "/run/houfeng/records-authority", True)] + binary_mounts
        center = start_runtime("center", "/usr/local/bin/houfeng-center", env_center, app_mounts, "10001:10001")
        await_tcp(center, ("127.0.0.1", center_port))
        processor = start_runtime("processor", "/usr/local/bin/houfeng-content-processor", env_processor, app_mounts, "10001:10001", ["--tmpfs", "/var/lib/houfeng/processor-workspace:rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=10001,gid=10001"])
        root_before = invoke(["docker", "exec", "--user", "10001:10001", center, "stat", "-c", "%d:%i", "/var/lib/houfeng/attachments"]).decode().strip()
        fds_before = {label: len(invoke(["docker", "exec", "--user", "10001:10001", n, "ls", "/proc/1/fd"]).splitlines()) for label, n in (("center", center), ("processor", processor))}
        os.environ["HOUFENG_SMOKE_HTTP_EVENTS"] = str(run_dir / "http-events.jsonl")
        spec = importlib.util.spec_from_file_location("http_attachment_smoke", HERE / "http-attachment.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        old_argv = sys.argv
        try:
            sys.argv = [str(HERE / "http-attachment.py"), base, str(certificate), str(credential)]
            http_result = module.run()
        finally: sys.argv = old_argv
        stop(processor, "processor")
        client = module.Client(base, str(certificate))
        client.json(200, "POST", "/api/auth/login", module.credentials(str(credential)))
        interrupted_stream, interrupted = module.begin_interrupted_upload(client, str(certificate), http_result["record_id"], http_result["revision_id"])
        upload_id = interrupted["upload_id"]
        if not re.fullmatch(r"aup_[a-z0-9]{1,64}", upload_id): raise RuntimeFailure("invalid upload identity")
        query = "select row_to_json(q) from (select obligation_id,record_id,temporary_key,temporary_device,temporary_inode,dispatched_at is not null as dispatched,temporary_created_at is not null as temp_created,returned_at is not null as returned,cleanup_completed_at is not null as cleaned,outcome from public.record_write_obligations where origin_kind='upload' and origin_id='" + upload_id + "') q"
        def obligation():
            raw = invoke(["docker", "run", "--rm", "--network=host", "--env-file", str(observer_env), "postgres:16-alpine", "psql", "-XAtq", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-p", str(db_port), "-U", "houfeng_runtime", "-d", "houfeng", "-c", query], timeout=10).strip()
            return json.loads(raw) if raw else None
        observed = None
        deadline = time.monotonic() + 18
        while time.monotonic() < deadline:
            row = obligation()
            if row and row["record_id"] == http_result["record_id"] and row["dispatched"] and row["temp_created"] and not row["returned"] and re.fullmatch(r"rwo_[0-9a-f]{32}", row["obligation_id"]):
                temporary = "sha256/.blob-tmp-" + row["obligation_id"]
                if row["temporary_key"] != temporary: raise RuntimeFailure("unexpected temporary identity")
                candidate = subprocess.run(["docker", "exec", "--user", "10001:10001", center, "stat", "-c", "%d:%i:%s", "/var/lib/houfeng/attachments/" + temporary], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
                if candidate.returncode == 0:
                    device, inode, size = map(int, candidate.stdout.strip().split(b":"))
                    if device == row["temporary_device"] and inode == row["temporary_inode"] and size == interrupted["sent_bytes"]:
                        expected = (b"interrupted managed bytes\n" * (size // len(b"interrupted managed bytes\n") + 1))[:size]
                        expected_hash = hashlib.sha256(expected).hexdigest()
                        actual_hash = invoke(["docker", "exec", "--user", "10001:10001", center, "sha256sum", "/var/lib/houfeng/attachments/" + temporary]).decode().split()[0]
                        if actual_hash != expected_hash: raise RuntimeFailure("partial temporary bytes differ before process death")
                        observed = row
                        interrupted.update({"obligation_id": row["obligation_id"], "temporary_key": temporary, "temporary_device": device, "temporary_inode": inode, "temporary_size_before_kill": size, "temporary_sha256_before_kill": actual_hash})
                        break
            time.sleep(0.1)
        if observed is None: raise RuntimeFailure("did not observe registered partial managed file before HTTP deadline")
        events.append({"stage": "managed-write-before-kill", **interrupted, "obligation": observed})
        invoke(["docker", "kill", "--signal", "KILL", center])
        killed_status = int(invoke(["docker", "wait", center]).strip())
        interrupted_stream.close()
        events.append({"stage": "kill", "process": "center", "signal": "SIGKILL", "exit_code": killed_status})
        if killed_status != 137: raise RuntimeFailure("center did not exit by SIGKILL")
        invoke(["docker", "start", center])
        await_tcp(center, ("127.0.0.1", center_port))
        invoke(["docker", "start", processor])
        if not alive(processor): raise RuntimeFailure("processor failed to restart")
        root_after = invoke(["docker", "exec", "--user", "10001:10001", center, "stat", "-c", "%d:%i", "/var/lib/houfeng/attachments"]).decode().strip()
        if root_before != root_after: raise RuntimeFailure("BlobRoot identity changed across restart")
        fds_after = {label: len(invoke(["docker", "exec", "--user", "10001:10001", n, "ls", "/proc/1/fd"]).splitlines()) for label, n in (("center", center), ("processor", processor))}
        recovered = obligation()
        if not recovered or recovered["obligation_id"] != interrupted["obligation_id"] or recovered["temporary_inode"] != interrupted["temporary_inode"] or recovered["temporary_device"] != interrupted["temporary_device"]:
            raise RuntimeFailure("interrupted obligation identity was not recovered")
        remaining = subprocess.run(["docker", "exec", "--user", "10001:10001", center, "stat", "-c", "%d:%i:%s", "/var/lib/houfeng/attachments/" + interrupted["temporary_key"]], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        if remaining.returncode:
            raise RuntimeFailure("interrupted temporary file is unavailable for deletion smoke")
        else:
            device, inode, size = map(int, remaining.stdout.strip().split(b":"))
            if device != interrupted["temporary_device"] or inode != interrupted["temporary_inode"] or size != interrupted["temporary_size_before_kill"]: raise RuntimeFailure("interrupted temporary identity or size changed")
            recovered_hash = invoke(["docker", "exec", "--user", "10001:10001", center, "sha256sum", "/var/lib/houfeng/attachments/" + interrupted["temporary_key"]]).decode().split()[0]
            if recovered_hash != interrupted["temporary_sha256_before_kill"]: raise RuntimeFailure("interrupted temporary bytes changed after restart")
            temporary_after = {"device": device, "inode": inode, "size": size, "sha256": recovered_hash}
        events.append({"stage": "managed-write-restart-readback", "obligation": recovered, "temporary": temporary_after, "note": "retained pending responsibility is not physical cleanup completion"})
        events.append({"stage": "descriptors-and-root", "root_device_inode": root_before, "fds_before": fds_before, "fds_after": fds_after})
        client = module.Client(base, str(certificate))
        client.json(200, "POST", "/api/auth/login", module.credentials(str(credential)))
        for attachment in (http_result["attachment_id"], http_result["imported_attachment_id"]):
            _, body = client.expect(200, "GET", "/api/attachments/" + attachment + "/content")
            if hashlib.sha256(body).hexdigest() != http_result["content_sha256"] or len(body) != http_result["content_bytes"]:
                raise RuntimeFailure("download differs after same-APP restart")
        post_restart = module.post_restart_upload(client)
        events.append({"stage": "post-restart-processor-upload", **post_restart})
        events.append({"stage": "restart-readback", "processes": ["center", "processor"], "same_app_and_namespace": True})
        stop(processor, "processor-before-deletion")
        deletion_private.mkdir(mode=0o700)
        issue_dir = deletion_private / "issue"
        replay_dir = deletion_private / "replay"
        issue_dir.mkdir(mode=0o700)
        replay_dir.mkdir(mode=0o700)
        request = {
            "run_id": os.environ["HOUFENG_RECORDS_RUN_ID"],
            "owner": os.environ["HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER"],
            "runtime_url": common["HOUFENG_DATABASE_URL"],
            "worker_url": "postgres://houfeng_record_deletion_worker:" + worker_password + "@127.0.0.1:" + str(db_port) + "/houfeng?sslmode=disable",
            "driver_binary_sha256": measurements["deletion-smoke"],
            "center_binary_sha256": measurements["houfeng-center"],
            "actor_id": http_result["actor_id"], "record_id": http_result["record_id"],
            "attachment_id": http_result["attachment_id"],
            "shared_sha256": http_result["content_sha256"],
            "exclusive_sha256": http_result["exclusive_content_sha256"],
            "preview_sha256": http_result["exclusive_preview_sha256"],
            "temporary_key": interrupted["temporary_key"], "obligation_id": interrupted["obligation_id"],
            "temporary_device": interrupted["temporary_device"], "temporary_inode": interrupted["temporary_inode"],
            "temporary_size": interrupted["temporary_size_before_kill"],
            "temporary_sha256": interrupted["temporary_sha256_before_kill"],
        }
        for phase, directory in (("issue", issue_dir), ("replay", replay_dir)):
            save_private(directory / "request.json", json.dumps(dict(request, phase=phase)))
        invoke(["docker", "run", "--rm", "--user", "0:0", "--network=none", "--mount", mounted(deletion_private, "/private", False), "--entrypoint", "/bin/chown", IMAGE, "-R", "10001:10001", "/private"])
        def deletion_phase(phase, directory):
            container = name("deletion-" + phase)
            cmd = ["docker", "run", "--name", container, "--network=host", "--user", "10001:10001", "--cap-drop=ALL", "--security-opt", "no-new-privileges"]
            for key in ("HOUFENG_POSTGRES_INTEGRATION", "HOUFENG_RECORDS_RUN_ID", "HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER"):
                cmd += ["--env", key]
            for src, dst, ro in app_mounts + [(directory, "/request", False)]:
                cmd += ["--mount", mounted(src, dst, ro)]
            if phase == "replay":
                cmd += ["--mount", mounted(issue_dir / "state.json", "/request/state.json")]
            cmd += ["--entrypoint", "/usr/local/bin/deletion-smoke", IMAGE, "--request-file", "/request/request.json"]
            result = json.loads(invoke(cmd, timeout=650))
            if result.get("status") != "PASS" or result.get("phase") != phase:
                raise RuntimeFailure("deletion driver returned incomplete " + phase + " evidence")
            events.append({"stage": "restricted-deletion-" + phase, **result})
            return result
        issued = deletion_phase("issue", issue_dir)
        if (issued.get("stream_attachment_id") != http_result["attachment_id"]
                or issued.get("stream_first_chunk_bytes") != 16
                or issued.get("stream_total_bytes") != http_result["content_bytes"]
                or issued.get("stream_barrier_held") is not True
                or issued.get("stream_drained") is not True):
            raise RuntimeFailure("deletion driver did not establish and drain the target content stream")
        stop(center, "center-before-deletion-replay")
        invoke(["docker", "start", center])
        await_tcp(center, ("127.0.0.1", center_port))
        invoke(["docker", "start", processor])
        if not alive(processor): raise RuntimeFailure("processor failed to restart for deletion replay")
        replay = deletion_phase("replay", replay_dir)
        if replay.get("operation_id") != issued.get("operation_id") or replay.get("receipt") != issued.get("receipt"):
            raise RuntimeFailure("deletion replay did not preserve operation and receipt")
        client = module.Client(base, str(certificate))
        client.json(200, "POST", "/api/auth/login", module.credentials(str(credential)))
        _, shared = client.expect(200, "GET", "/api/attachments/" + http_result["imported_attachment_id"] + "/content")
        if len(shared) != http_result["content_bytes"] or hashlib.sha256(shared).hexdigest() != http_result["content_sha256"]:
            raise RuntimeFailure("surviving record's shared attachment changed")
        for route in ("permanent-delete-preview", "permanent-delete"):
            _, disabled = client.json(503, "POST", "/api/records/" + http_result["record_id"] + "/" + route, {})
            if disabled != {"code": "deletion_safety_unavailable", "message": "deletion safety unavailable", "field_errors": []}:
                raise RuntimeFailure("permanent deletion HTTP route did not remain closed")
        stop(processor, "processor-final")
        stop(center, "center-final")
        stop(authority, "authority")
        completed = True
        return {"status": "PASS", "http": http_result, "lifecycle": events, "deletion": {"status": "PASS", "issue": issued, "replay": replay, "shared_http_readable": True, "permanent_delete_http_status": 503}}
    finally:
        for n in names:
            try:
                state_info = json.loads(invoke(["docker", "inspect", "--format", "{{json .State}}", n], timeout=15))
                events.append({"stage": "container-final-state", "container": n, **{key: state_info.get(key) for key in ("Status", "Running", "ExitCode", "OOMKilled", "StartedAt", "FinishedAt")}})
            except (OSError, ValueError, subprocess.SubprocessError, RuntimeFailure):
                events.append({"stage": "container-state-capture-failed", "container": n})
            try:
                logs = subprocess.run(["docker", "logs", n], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15).stdout
                for value in redact:
                    if value: logs = logs.replace(value.encode(), b"[redacted]")
                fd = os.open(run_dir / (n + ".log"), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(fd, "wb") as f: f.write(logs)
            except (OSError, subprocess.SubprocessError):
                events.append({"stage": "log-capture-failed", "container": n})
        for n in reversed(names):
            try:
                cleaned = subprocess.run(["docker", "rm", "-f", n], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
                if cleaned.returncode: events.append({"stage": "container-cleanup-failed", "container": n, "status": cleaned.returncode})
            except subprocess.SubprocessError:
                events.append({"stage": "container-cleanup-failed", "container": n, "status": "timeout"})
        if deletion_private.exists():
            try:
                cleaned = subprocess.run(["docker", "run", "--rm", "--user", "0:0", "--network=none", "--mount", mounted(deletion_private, "/private", False), "--entrypoint", "/bin/sh", IMAGE, "-ec", "rm -f /private/issue/request.json /private/issue/state.json /private/replay/request.json"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
                if cleaned.returncode: events.append({"stage": "private-cleanup-failed"})
            except subprocess.SubprocessError:
                events.append({"stage": "private-cleanup-failed"})
        save_private(run_dir / "runtime-events.json", json.dumps(events, sort_keys=True, indent=2) + "\n")
        if forwarder: forwarder.close()
        for file in ephemeral_files:
            try: file.unlink()
            except FileNotFoundError: pass
        if completed and any(event.get("stage") in ("container-cleanup-failed", "private-cleanup-failed", "container-state-capture-failed", "log-capture-failed") for event in events):
            raise RuntimeFailure("runtime evidence or cleanup failed")
