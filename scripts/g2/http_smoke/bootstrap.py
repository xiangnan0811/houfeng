#!/usr/bin/env python3
"""Isolated production Compose initialization and restricted deletion smoke.

Run only as the child of scripts/test-record-platform-integration.sh. The
fixed Compose endpoint db:5432 is forwarded solely to that fixture's loopback
APP endpoint.
"""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import stat
import socket
import subprocess
import sys
import threading
import time
from urllib.parse import urlsplit, unquote
import uuid
import runtime

HERE = Path(__file__).resolve().parent
CHECKOUT = HERE.parents[2]
BINARY_NAMES = ("houfeng-center", "houfeng-content-processor", "houfeng-record-platform-admin", "deletion-smoke")
BASE_IMAGE = "linnea7171/houfeng:v1.19.0"
PG_IMAGE = "postgres:16-alpine"

class Failure(Exception):
    pass

def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

def private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(value)

def command(args, log=None, timeout=300):
    # Commands contain paths and roles only. Credentials are in private files.
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if log is not None:
        fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as f:
            f.write(result.stdout)
    if result.returncode:
        raise Failure(f"{args[0]} operation failed (status {result.returncode}); private diagnostic: {log}")
    return result.stdout

def btrfs(path):
    # Linux statfs starts with f_type, which is a signed long.
    buf = ctypes.create_string_buffer(512)
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.statfs(os.fsencode(path), ctypes.byref(buf)) != 0:
        raise OSError(ctypes.get_errno(), "statfs")
    return ctypes.c_long.from_buffer(buf).value == 0x9123683E

def smoke_run_root():
    raw = os.environ.get("G2_SMOKE_RUN_ROOT", "")
    if not raw:
        raise Failure("G2_SMOKE_RUN_ROOT must identify an owned run directory")
    root = Path(raw)
    if not root.is_absolute() or ".." in root.parts:
        raise Failure("G2_SMOKE_RUN_ROOT must be an absolute path without parent traversal")
    agent = CHECKOUT / "tmp" / "agent"
    if root == agent or not root.is_relative_to(agent) or root.is_relative_to(CHECKOUT / "scripts"):
        raise Failure("G2_SMOKE_RUN_ROOT must be inside checkout tmp/agent")
    try:
        for ancestor in reversed((root, *root.parents)):
            info = ancestor.lstat()
            if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
                raise Failure("G2_SMOKE_RUN_ROOT and its ancestors must be non-symlink directories")
        if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
            raise Failure("G2_SMOKE_RUN_ROOT must be owned by the current uid and mode 0700")
    except OSError as exc:
        raise Failure("G2_SMOKE_RUN_ROOT must be an existing accessible directory") from exc
    return root

class Proxy:
    def __init__(self, target_port):
        self.listener = socket.socket()
        # No SO_REUSEPORT and no fallback to another database endpoint.
        self.listener.bind(("127.0.0.1", 5432))
        self.listener.listen(32)
        self.listener.settimeout(0.5)
        self.target_port = target_port
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()
    def accept(self):
        while not self.stop_event.is_set():
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            threading.Thread(target=self.connection, args=(client,), daemon=True).start()
    def connection(self, client):
        try:
            server = socket.create_connection(("127.0.0.1", self.target_port), timeout=5)
            server.settimeout(None)
            def copy(a, b):
                try:
                    while True:
                        data = a.recv(65536)
                        if not data:
                            break
                        b.sendall(data)
                except OSError:
                    pass
                finally:
                    try: b.shutdown(socket.SHUT_WR)
                    except OSError: pass
            t = threading.Thread(target=copy, args=(client, server), daemon=True)
            t.start()
            copy(server, client)
            t.join(timeout=10)
            server.close()
        except OSError:
            pass
        finally:
            client.close()
    def close(self):
        self.stop_event.set()
        self.listener.close()
        self.thread.join(timeout=2)

def run():
    run_root = smoke_run_root()
    if len(sys.argv) != 1 or os.environ.get("HOUFENG_POSTGRES_INTEGRATION") != "1" or not os.environ.get("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") or not os.environ.get("HOUFENG_RECORDS_RUN_ID"):
        raise Failure("invoke only under the repository's isolated record-platform PostgreSQL runner")
    raw = os.environ.get("HOUFENG_DATABASE_URL", "")
    parsed = urlsplit(raw)
    if parsed.scheme not in ("postgres", "postgresql") or parsed.hostname != "127.0.0.1" or parsed.username != "postgres" or parsed.path != "/postgres" or parsed.query != "sslmode=disable" or not parsed.port or not parsed.password:
        raise Failure("APP fixture URL does not match the isolated runner contract")
    binary_dir = os.environ.get("G2_SMOKE_BIN_DIR", "")
    if not binary_dir:
        raise Failure("G2_SMOKE_BIN_DIR must identify this run's new binary directory")
    binaries = Path(binary_dir).resolve(strict=True)
    if not binaries.is_dir():
        raise Failure("G2_SMOKE_BIN_DIR is not a directory")
    measurements = {}
    for name in BINARY_NAMES:
        executable = binaries / name
        if not executable.is_file():
            raise Failure("missing measured executable: " + str(executable))
        with executable.open("rb") as file:
            if file.read(4) != b"\x7fELF":
                raise Failure("measured executable is not an ELF: " + str(executable))
        measurements[name] = digest(executable)
    if not btrfs(run_root):
        raise Failure("the owned scratch filesystem is not btrfs")
    run_dir = run_root / ("run-" + secrets.token_hex(8))
    run_dir.mkdir(mode=0o700)
    state, files, logs = (run_dir / x for x in ("state", "private", "logs"))
    for directory in (state, files, logs): directory.mkdir(mode=0o700)
    result = {"status": "NOT_READY", "scope": "actual initialization, HTTP and restricted V2 deletion smoke", "commands": [], "executable_sha256": measurements, "stages": {}, "run_dir": str(run_dir)}
    result_path = run_dir / "result.json"
    pw = {name: secrets.token_hex(24) for name in ("runtime", "admin", "worker", "migrator")}
    secret_values = {"postgres_bootstrap_password": unquote(parsed.password), "houfeng_runtime_password": pw["runtime"], "houfeng_platform_admin_password": pw["admin"], "houfeng_record_deletion_worker_password": pw["worker"], "houfeng_migrator_password": pw["migrator"]}
    containers = []
    proxy = None
    def container(label, uid, args, mounts, env=None):
        name = "g2arc-main-" + secrets.token_hex(10)
        containers.append(name)
        cmd = ["docker", "run", "--name", name, "--network=host", "--add-host", "db:127.0.0.1", "--user", uid, "--cap-drop=ALL", "--security-opt", "no-new-privileges"]
        if uid == "10002:10002": cmd += ["--group-add", "10001"]
        for source, target, mode in mounts:
            suffix = ",readonly" if mode in ("ro", "readonly") else ""
            cmd += ["--mount", f"type=bind,src={source},dst={target}{suffix}"]
        if env: cmd += ["--env-file", str(env)]
        cmd += ["--entrypoint", args[0], BASE_IMAGE] + args[1:]
        result["commands"].append({"stage": label, "executable": args[0], "argv": args[1:], "uid": uid})
        return command(cmd, logs / (label + ".log"))
    def finish():
        nonlocal proxy
        for name in reversed(containers):
            try:
                closed = subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
                if closed.returncode: result["stages"]["container_cleanup_" + name] = "failed"
            except subprocess.SubprocessError:
                result["stages"]["container_cleanup_" + name] = "timeout"
        if proxy: proxy.close()
        # Root cleanup is limited to this run's private input directories;
        # they were chowned to the production UIDs and are inaccessible to
        # the host user. Do not touch the signed authority state.
        cleanup = ["docker", "run", "--rm", "--user", "0:0", "--network=none", "--mount", f"type=bind,src={files},dst=/private"]
        request_dir = run_dir / "request"
        if request_dir.exists(): cleanup += ["--mount", f"type=bind,src={request_dir},dst=/request"]
        cleanup += ["--entrypoint", "/bin/sh", BASE_IMAGE, "-ec", "rm -f /private/*; if [ -d /request ]; then rm -f /request/request.json; fi"]
        try:
            cleaned = subprocess.run(cleanup, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30).returncode == 0
        except subprocess.SubprocessError:
            cleaned = False
        result["stages"]["private_input_cleanup"] = cleaned
        required = ("isolated_database_created", "production_deploy_init", "production_local_namespace", "actual_blobroot_btrfs", "private_input_cleanup")
        if (not result.get("failure_class") and all(result["stages"].get(stage) is True for stage in required)
                and not any(key.startswith("container_cleanup_") for key in result["stages"])
                and result.get("runtime", {}).get("status") == "PASS"
                and result["runtime"].get("deletion", {}).get("status") == "PASS"):
            result["status"] = "PASS"
        result_path.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
        os.chmod(result_path, 0o600)
    try:
        for key, value in secret_values.items(): private(files / key, value)
        private(files / "pg.env", "PGPASSWORD=" + unquote(parsed.password) + "\n")
        proxy = Proxy(parsed.port)
        # The isolated APP starts as postgres; this database is owned by the
        # runner's disposable PostgreSQL container, never by a business process.
        command(["docker", "run", "--rm", "--network=host", "--env-file", str(files / "pg.env"), PG_IMAGE, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-p", str(parsed.port), "-U", "postgres", "-d", "postgres", "-c", "create database houfeng"], logs / "create-database.log")
        result["stages"]["isolated_database_created"] = True
        # Root is confined to the owned bind mounts for ownership preparation.
        command(["docker", "run", "--rm", "--user", "0:0", "--network=none", "--mount", f"type=bind,src={state},dst=/state", "--mount", f"type=bind,src={files},dst=/private", "--entrypoint", "/bin/sh", BASE_IMAGE, "-ec", "install -d -o 10002 -g 10002 -m 0755 /state/center-config; install -d -o 10002 -g 10002 -m 0750 /state/maintenance; install -d -o 10001 -g 10001 -m 0700 /state/attachments; chown 10002:10002 /state; chmod 0750 /state; chown -R 10002:10002 /private"], logs / "prepare-ownership.log")
        mounts = [(state, "/var/lib/houfeng", "rw"), (state / "maintenance", "/var/lib/houfeng-maintenance", "rw"), (files, "/run/secrets", "ro")] + [(binaries / name, "/usr/local/bin/" + name, "readonly") for name in BINARY_NAMES]
        container("deploy-init", "10002:10002", ["/usr/local/bin/houfeng-record-platform-admin", "deploy-init", "--scope", "compose"], mounts)
        result["stages"]["production_deploy_init"] = True
        request = {"database_url": f"postgres://houfeng_platform_admin:{pw['admin']}@127.0.0.1:{parsed.port}/houfeng?sslmode=disable", "root": "/var/lib/houfeng/attachments", "namespace_id": str(uuid.uuid4()), "incarnation_id": str(uuid.uuid4()), "request_id": str(uuid.uuid4()), "owner_uid": 10001, "owner_gid": 10001}
        # Request directory is separate because the production command consumes it.
        req_dir = run_dir / "request"
        req_dir.mkdir(mode=0o700)
        private(req_dir / "request.json", json.dumps(request))
        command(["docker", "run", "--rm", "--user", "0:0", "--network=none", "--mount", f"type=bind,src={req_dir},dst=/request", "--entrypoint", "/bin/chown", BASE_IMAGE, "-R", "10001:10001", "/request"], logs / "prepare-request.log")
        namespace_output = container("local-namespace", "10001:10001", ["/usr/local/bin/houfeng-record-platform-admin", "local-namespace", "provision", "--request-file", "/request/request.json"], [(state / "attachments", "/var/lib/houfeng/attachments", "rw"), (req_dir, "/request", "rw"), (binaries / "houfeng-record-platform-admin", "/usr/local/bin/houfeng-record-platform-admin", "readonly")])
        namespace = json.loads(namespace_output)
        if namespace != {"namespace_id": request["namespace_id"], "incarnation_id": request["incarnation_id"], "request_id": request["request_id"], "admission_state": "active"}:
            raise Failure("local namespace CLI did not confirm the exact active binding")
        command(["docker", "run", "--rm", "--network=none", "--mount", f"type=bind,src={req_dir},dst=/request,readonly", "--mount", f"type=bind,src={state / 'attachments'},dst=/blob,readonly", "--entrypoint", "/bin/sh", BASE_IMAGE, "-ec", "test ! -e /request/request.json; test \"$(stat -f -c %t /blob)\" = 9123683e"], logs / "namespace-readback.log")
        result["stages"]["production_local_namespace"] = True
        result["stages"]["actual_blobroot_btrfs"] = True
        result["namespace"] = {k: request[k] for k in ("namespace_id", "incarnation_id", "request_id")}
        result["runtime"] = runtime.run(state, binaries, run_dir, parsed.port, pw["runtime"], pw["worker"], measurements)
    except Exception as exc:
        result["failure_class"] = type(exc).__name__
        if isinstance(exc, (Failure, runtime.RuntimeFailure)) or type(exc).__name__ == "SmokeError":
            result["failure_detail"] = str(exc)
        raise
    finally:
        finish()
    print(json.dumps(result, sort_keys=True))

if __name__ == "__main__":
    def interrupted(_signal, _frame):
        raise RuntimeError("interrupted")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(1800)
    try:
        run()
    except Exception as exc:
        print("isolated bootstrap or runtime failed; class:", type(exc).__name__, file=sys.stderr)
        sys.exit(1)
