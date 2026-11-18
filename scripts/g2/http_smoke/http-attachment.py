#!/usr/bin/env python3
"""Authenticated HTTPS attachment segment against an already running isolated center.

This segment uses real HTTPS and production APIs. Draft handoff revocation is
distinct from a deletion fence or a record serving-lease revocation.
"""
import hashlib
import http.cookiejar
import http.client
import socket
import json
import io
import zipfile
import os
from pathlib import Path
import ssl
import stat
import struct
import zlib
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

class SmokeError(Exception):
    pass

class Client:
    def __init__(self, base, ca_file):
        parsed = urllib.parse.urlsplit(base)
        if parsed.scheme != "https" or parsed.hostname not in ("localhost", "127.0.0.1", "::1") or parsed.path not in ("", "/") or parsed.query or parsed.fragment or parsed.username:
            raise SmokeError("HTTPS base must be an isolated loopback origin")
        self.base = base.rstrip("/")
        self.jar = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar), urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=ca_file)))
        self.events = []
    def record(self, event):
        self.events.append(event)
        path = os.environ.get("HOUFENG_SMOKE_HTTP_EVENTS")
        if path:
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_CLOEXEC, 0o600)
            with os.fdopen(fd, "a") as file: file.write(json.dumps(event, sort_keys=True) + "\n")
    def request(self, method, path, body=None, headers=None):
        if not path.startswith("/api/") or path.startswith("//"):
            raise SmokeError("unexpected API path")
        hdr = {"Origin": self.base}
        if headers: hdr.update(headers)
        if isinstance(body, dict):
            body = json.dumps(body, separators=(",", ":")).encode()
            hdr["Content-Type"] = "application/json"
        req = urllib.request.Request(self.base + path, data=body, method=method, headers=hdr)
        try:
            response = self.http.open(req, timeout=30)
        except urllib.error.HTTPError as err:
            response = err
        with response:
            payload = response.read(16 * 1024 * 1024 + 1)
            if len(payload) > 16 * 1024 * 1024:
                raise SmokeError("HTTP response exceeds bounded capture")
            self.record({"method": method, "path": path.split("?")[0], "status": response.status, "bytes": len(payload)})
            return response.status, dict(response.headers), payload
    def expect(self, expected, method, path, body=None, headers=None):
        code, response_headers, raw = self.request(method, path, body, headers)
        if code != expected:
            raise SmokeError(f"{method} {path.split('?')[0]} returned HTTP {code}, expected {expected}")
        return response_headers, raw
    def json(self, expected, method, path, body=None, headers=None):
        h, raw = self.expect(expected, method, path, body, headers)
        try: return h, json.loads(raw)
        except (ValueError, UnicodeError) as exc: raise SmokeError("invalid JSON response") from exc

def credentials(path):
    # Caller supplies private one-shot credentials, never on argv or in results.
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        s = os.fstat(fd)
        if not stat.S_ISREG(s.st_mode) or s.st_uid != os.geteuid() or stat.S_IMODE(s.st_mode) != 0o600 or s.st_nlink != 1 or s.st_size > 4096:
            raise SmokeError("credentials file must be an owned 0600 single-link file")
        with os.fdopen(fd, "rb", closefd=False) as f: obj = json.load(f)
    finally: os.close(fd)
    if set(obj) != {"username", "password"} or not all(isinstance(obj[x], str) and obj[x] for x in obj):
        raise SmokeError("invalid credentials file")
    return obj

def draft_handoff_revoke(client, ca_file, source_id):
    payload = {"title": "handoff stream", "body_markdown": "# Stream\n", "markdown_dialect_version": 1, "record_type": "note", "business_status": "", "impact_level": "low", "visibility": {"kind": "project", "allowed_roles": [], "allowed_group_ids": []}, "subjects": [{"registry_version": 1, "kind": "vps", "role": "affected", "source_id": source_id, "primary": True}], "tags": [], "owner_id": "", "participant_ids": [], "attachment_ids": [], "save_reason": "handoff smoke"}
    _, draft = client.json(201, "POST", "/api/record-drafts", {"payload": payload})
    draft_id = draft["draft_id"]
    # Large enough to exceed ordinary TCP send/receive buffering; a completed
    # response is explicitly rejected rather than counted as a revocation.
    content = b"ordinary content for draft handoff\n" * (1024 * 1024)
    digest = hashlib.sha256(content).hexdigest()
    _, upload = client.json(201, "POST", "/api/attachment-uploads", {"draft_id": draft_id, "display_name": "handoff.txt", "media_type": "text/plain", "declared_size_bytes": len(content)})
    upload_id, attachment_id = upload["upload_id"], upload["attachment_id"]
    client.expect(200, "PUT", "/api/attachment-uploads/" + upload_id + "/content", content, {"Content-Type": "application/octet-stream", "X-Houfeng-Draft-ID": draft_id, "X-Content-SHA256": digest})
    client.json(202, "POST", "/api/attachment-uploads/" + upload_id + "/complete", {"draft_id": draft_id})
    deadline = time.monotonic() + 180
    while True:
        _, metadata = client.json(200, "GET", "/api/attachments/" + attachment_id)
        if metadata["state"] == "available": break
        if metadata["state"] in ("rejected", "expired") or time.monotonic() >= deadline: raise SmokeError("handoff attachment did not become available")
        time.sleep(0.5)
    payload["attachment_ids"] = [attachment_id]
    _, draft = client.json(200, "PATCH", "/api/record-drafts/" + draft_id, {"payload": payload}, {"If-Match": draft["etag"]})
    parsed = urllib.parse.urlsplit(client.base)
    connection = http.client.HTTPSConnection(parsed.hostname, parsed.port, context=ssl.create_default_context(cafile=ca_file), timeout=60)
    connection.connect()
    connection.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 65536)
    receive_buffer = connection.sock.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF)
    cookie = "; ".join(c.name + "=" + c.value for c in client.jar)
    if not cookie: raise SmokeError("stream requires authenticated cookie")
    try:
        connection.request("GET", "/api/attachments/" + attachment_id + "/content", headers={"Cookie": cookie, "Connection": "close"})
        response = connection.getresponse()
        if response.status != 200 or int(response.getheader("Content-Length", "0")) != len(content): raise SmokeError("handoff stream was not opened with full content length")
        first = response.read(32768)
        if not first or first != content[:len(first)]: raise SmokeError("handoff stream first bytes differ")
        first_observed = time.monotonic_ns()
        client.record({"stage": "handoff-stream-open", "attachment_id": attachment_id, "declared_bytes": len(content), "first_bytes": len(first), "first_observed_monotonic_ns": first_observed, "socket_receive_buffer": receive_buffer})
        result = {"count": len(first), "hash": hashlib.sha256(first), "ended_ns": 0, "error": None, "terminal": None, "incomplete_partial_bytes": 0}
        def drain():
            try:
                while result["count"] < len(content):
                    try:
                        chunk = response.read(65536)
                    except http.client.IncompleteRead as exc:
                        chunk = exc.partial
                        result["incomplete_partial_bytes"] = len(chunk)
                        result["terminal"] = "incomplete_read"
                    if chunk:
                        result["count"] += len(chunk)
                        result["hash"].update(chunk)
                    if result["terminal"]: break
                    if not chunk:
                        result["terminal"] = "eof"
                        break
                if result["count"] == len(content): result["terminal"] = "full_content"
            except (OSError, http.client.HTTPException) as exc:
                result["error"] = type(exc).__name__
                result["terminal"] = "read_error"
            finally: result["ended_ns"] = time.monotonic_ns()
        # Keep the client paused until the handoff response is received, so
        # concurrent draining cannot race ahead of the ownership change.
        commit_started = time.monotonic_ns()
        _, committed = client.json(201, "POST", "/api/records", {"draft_id": draft_id, "draft_etag": draft["etag"]}, {"Idempotency-Key": "g2arc-stream-handoff"})
        committed_at = time.monotonic_ns()
        reader = threading.Thread(target=drain, daemon=True)
        reader.start()
        reader.join(timeout=45)
        if reader.is_alive():
            client.record({"stage": "handoff-stream-result", "terminal": "reader_timeout", "declared_bytes": len(content), "bytes_observed_total": result["count"], "commit_started_monotonic_ns": commit_started, "commit_observed_monotonic_ns": committed_at})
            raise SmokeError("draft handoff stream did not terminate before deadline")
        count = result["count"]
        observed_hash = result["hash"].hexdigest()
        expected_hash = hashlib.sha256(content[:count]).hexdigest()
        client.record({"stage": "handoff-stream-result", "terminal": result["terminal"], "error": result["error"], "declared_bytes": len(content), "bytes_observed_total": count, "observed_sha256": observed_hash, "expected_prefix_sha256": expected_hash, "incomplete_partial_bytes": result["incomplete_partial_bytes"], "commit_started_monotonic_ns": commit_started, "commit_observed_monotonic_ns": committed_at, "stream_ended_monotonic_ns": result["ended_ns"]})
        if count >= len(content): raise SmokeError("draft handoff stream delivered the full content")
        if observed_hash != expected_hash: raise SmokeError("draft handoff stream bytes differ from the expected prefix")
        if result["error"]: raise SmokeError("draft handoff stream read failed: " + result["error"])
        # A fresh request using the same session must still receive all bytes
        # from the attachment's new record route.
        control = http.client.HTTPSConnection(parsed.hostname, parsed.port, context=ssl.create_default_context(cafile=ca_file), timeout=60)
        try:
            control.request("GET", "/api/attachments/" + attachment_id + "/content", headers={"Cookie": cookie, "Connection": "close"})
            check = control.getresponse()
            if check.status != 200: raise SmokeError("new record route denied the control download")
            control_hash, control_size = hashlib.sha256(), 0
            while True:
                part = check.read(65536)
                if not part: break
                control_hash.update(part)
                control_size += len(part)
            if control_size != len(content) or control_hash.hexdigest() != digest: raise SmokeError("record route control download differs")
        finally: control.close()
        return {"mechanism": "draft-to-record attachment ownership assertion", "not_deletion_fence": True, "record_id": committed["record_id"], "attachment_id": attachment_id, "declared_bytes": len(content), "bytes_before_handoff": len(first), "bytes_observed_total": count, "first_observed_monotonic_ns": first_observed, "commit_observed_monotonic_ns": committed_at, "stream_ended_monotonic_ns": result["ended_ns"], "stream_error_class": result["error"], "control_bytes": control_size, "control_sha256": control_hash.hexdigest()}
    finally:
        connection.close()

def begin_interrupted_upload(client, ca_file, record_id, revision_id):
    payload = {"title": "interrupted managed upload", "visibility": {"kind": "project", "allowed_roles": [], "allowed_group_ids": []}, "subjects": [], "tags": [], "participant_ids": [], "attachment_ids": []}
    _, draft = client.json(201, "POST", "/api/record-drafts", {"record_id": record_id, "base_revision_id": revision_id, "payload": payload})
    draft_id = draft["draft_id"]
    body = b"interrupted managed bytes\n" * (256 * 1024)
    _, upload = client.json(201, "POST", "/api/attachment-uploads", {"draft_id": draft_id, "display_name": "interrupted.txt", "media_type": "text/plain", "declared_size_bytes": len(body)})
    parsed = urllib.parse.urlsplit(client.base)
    connection = http.client.HTTPSConnection(parsed.hostname, parsed.port, context=ssl.create_default_context(cafile=ca_file), timeout=20)
    connection.connect()
    connection.putrequest("PUT", "/api/attachment-uploads/" + upload["upload_id"] + "/content")
    connection.putheader("Cookie", "; ".join(c.name + "=" + c.value for c in client.jar))
    connection.putheader("Origin", client.base)
    connection.putheader("Content-Type", "application/octet-stream")
    connection.putheader("Content-Length", str(len(body)))
    connection.putheader("X-Houfeng-Draft-ID", draft_id)
    connection.putheader("X-Content-SHA256", hashlib.sha256(body).hexdigest())
    connection.endheaders()
    connection.send(body[:1024 * 1024])
    return connection, {"draft_id": draft_id, "upload_id": upload["upload_id"], "declared_bytes": len(body), "sent_bytes": 1024 * 1024, "sha256": hashlib.sha256(body).hexdigest()}

def post_restart_upload(client):
    payload = {"title": "restart processor check", "visibility": {"kind": "project", "allowed_roles": [], "allowed_group_ids": []}, "subjects": [], "tags": [], "participant_ids": [], "attachment_ids": []}
    _, draft = client.json(201, "POST", "/api/record-drafts", {"payload": payload})
    content = b"processor after same-APP restart\n"
    digest = hashlib.sha256(content).hexdigest()
    _, upload = client.json(201, "POST", "/api/attachment-uploads", {"draft_id": draft["draft_id"], "display_name": "after-restart.txt", "media_type": "text/plain", "declared_size_bytes": len(content)})
    client.expect(200, "PUT", "/api/attachment-uploads/" + upload["upload_id"] + "/content", content, {"Content-Type": "application/octet-stream", "X-Houfeng-Draft-ID": draft["draft_id"], "X-Content-SHA256": digest})
    client.json(202, "POST", "/api/attachment-uploads/" + upload["upload_id"] + "/complete", {"draft_id": draft["draft_id"]})
    deadline = time.monotonic() + 120
    while True:
        _, metadata = client.json(200, "GET", "/api/attachments/" + upload["attachment_id"])
        if metadata["state"] == "available": break
        if metadata["state"] in ("rejected", "expired") or time.monotonic() >= deadline: raise SmokeError("restarted processor did not complete new upload")
        time.sleep(0.5)
    _, got = client.expect(200, "GET", "/api/attachments/" + upload["attachment_id"] + "/content")
    if got != content or not metadata.get("preview_available"): raise SmokeError("post-restart processing or exact download failed")
    return {"attachment_id": upload["attachment_id"], "sha256": digest, "bytes": len(content), "processor_state": metadata["state"], "preview_available": True}

def exclusive_png():
    def chunk(kind, data):
        return struct.pack("!I", len(data)) + kind + data + struct.pack("!I", zlib.crc32(kind + data) & 0xffffffff)
    rows = b"\0" + b"\x12\x34\x56\xff" * 2 + b"\0" + b"\x9a\xbc\xde\xff" * 2
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack("!IIBBBBB", 2, 2, 8, 6, 0, 0, 0)) +
            chunk(b"tEXt", b"Comment\0exclusive smoke") + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))

def run():
    if len(sys.argv) != 4:
        raise SmokeError("usage: http-attachment.py HTTPS_LOOPBACK_ORIGIN CA_FILE PRIVATE_CREDENTIAL_FILE")
    client = Client(sys.argv[1], sys.argv[2])
    client.json(200, "POST", "/api/auth/login", credentials(sys.argv[3]))
    if not client.jar:
        raise SmokeError("login returned no real session cookie")
    _, actor = client.json(200, "GET", "/api/auth/me")
    _, draft = client.json(201, "POST", "/api/record-drafts", {"payload": {"title": "isolated attachment smoke", "visibility": {"kind": "project", "allowed_roles": [], "allowed_group_ids": []}, "subjects": [], "tags": [], "participant_ids": [], "attachment_ids": []}})
    draft_id = draft["draft_id"]
    if not isinstance(draft_id, str) or not draft_id.startswith("rdf_"):
        raise SmokeError("unexpected draft identity")
    content = b"Houfeng isolated normal-main attachment smoke.\n"
    sha = hashlib.sha256(content).hexdigest()
    _, upload = client.json(201, "POST", "/api/attachment-uploads", {"draft_id": draft_id, "display_name": "smoke.txt", "media_type": "text/plain", "declared_size_bytes": len(content)})
    upload_id, attachment_id = upload["upload_id"], upload["attachment_id"]
    if not upload_id.startswith("aup_") or not attachment_id.startswith("att_"):
        raise SmokeError("unexpected upload identity")
    client.expect(200, "PUT", "/api/attachment-uploads/" + upload_id + "/content", content, {"Content-Type": "application/octet-stream", "X-Houfeng-Draft-ID": draft_id, "X-Content-SHA256": sha})
    _, completion = client.json(202, "POST", "/api/attachment-uploads/" + upload_id + "/complete", {"draft_id": draft_id})
    if completion["attachment_id"] != attachment_id:
        raise SmokeError("completion attachment identity changed")
    deadline = time.monotonic() + 120
    while True:
        _, metadata = client.json(200, "GET", "/api/attachments/" + attachment_id)
        if metadata["state"] == "available": break
        if metadata["state"] in ("rejected", "expired") or time.monotonic() >= deadline:
            raise SmokeError("processor did not produce an available attachment")
        time.sleep(0.5)
    _, downloaded = client.expect(200, "GET", "/api/attachments/" + attachment_id + "/content")
    if downloaded != content:
        raise SmokeError("downloaded bytes differ from uploaded bytes")
    if not metadata.get("preview_available"):
        raise SmokeError("text processor did not publish a preview")
    _, preview = client.expect(200, "GET", "/api/attachments/" + attachment_id + "/content?variant=preview")
    if not preview: raise SmokeError("preview is empty")
    # Create real subjects and publish the same draft through public APIs.
    _, source = client.json(201, "POST", "/api/vps", {"display_name": "Smoke source", "lifecycle_status": "active"})
    _, destination = client.json(201, "POST", "/api/vps", {"display_name": "Smoke destination", "lifecycle_status": "active"})
    source_id, destination_id = source["vps_id"], destination["vps_id"]
    payload = {"title": "isolated attachment smoke", "body_markdown": "# Isolated attachment smoke\n", "markdown_dialect_version": 1, "record_type": "note", "business_status": "", "impact_level": "low", "visibility": {"kind": "project", "allowed_roles": [], "allowed_group_ids": []}, "subjects": [{"registry_version": 1, "kind": "vps", "role": "affected", "source_id": source_id, "primary": True}], "tags": [], "owner_id": "", "participant_ids": [], "attachment_ids": [attachment_id], "save_reason": "isolated smoke"}
    _, draft = client.json(200, "PATCH", "/api/record-drafts/" + draft_id, {"payload": payload}, {"If-Match": draft["etag"]})
    _, record = client.json(201, "POST", "/api/records", {"draft_id": draft_id, "draft_etag": draft["etag"]}, {"Idempotency-Key": "g2arc-smoke-create"})
    record_id = record["record_id"]
    for route in ("permanent-delete-preview", "permanent-delete"):
        _, disabled = client.json(503, "POST", "/api/records/" + record_id + "/" + route, {})
        if disabled != {"code": "deletion_safety_unavailable", "message": "deletion safety unavailable", "field_errors": []}:
            raise SmokeError("disabled permanent deletion returned an unexpected error body")
        client.record({"stage": "permanent-delete-disabled", "route": route, "status": 503, "code": disabled["code"]})
    export_key = "g2arc-smoke-export"
    _, export_preview = client.json(200, "POST", "/api/record-export-previews", {"record_id": record_id, "export_kind": "archive", "export_mode": "safe"}, {"Idempotency-Key": export_key})
    export_request = {k: export_preview[k] for k in ("preview_id", "preview_token", "inventory_digest")}
    if export_preview.get("confirm_token"): export_request["confirm_token"] = export_preview["confirm_token"]
    _, export = client.json(201, "POST", "/api/record-exports", export_request, {"Idempotency-Key": export_key})
    _, archive = client.expect(200, "GET", "/api/record-exports/" + export["export_id"] + "/content")
    with zipfile.ZipFile(io.BytesIO(archive)) as z:
        member = "records/" + record_id + "/attachments/" + attachment_id + "/smoke.txt"
        if z.read(member) != content: raise SmokeError("export archive attachment bytes differ")
    query = urllib.parse.urlencode({"destination_subject_kind": "vps", "destination_subject_id": destination_id})
    _, plan_raw = client.expect(200, "POST", "/api/record-imports/dry-run?" + query, archive, {"Content-Type": "application/zip", "Idempotency-Key": "g2arc-smoke-import"})
    plan = json.loads(plan_raw)
    imported_ids = [r["target_id"] for r in plan["remaps"] if r["entity_kind"] == "attachment" and r["source_id"] == attachment_id]
    if len(imported_ids) != 1: raise SmokeError("import did not plan exactly one attachment remap")
    _, applied = client.json(200, "POST", "/api/record-imports/" + plan["plan_id"] + "/apply", {"lock_version": plan["lock_version"]})
    if not applied["record_ids"]: raise SmokeError("import produced no record")
    _, imported = client.expect(200, "GET", "/api/attachments/" + imported_ids[0] + "/content")
    if imported != content: raise SmokeError("imported attachment bytes differ")
    # Add an attachment only after the archive has been imported so the
    # imported record shares the first blob but cannot reference this one.
    _, edit = client.json(201, "POST", "/api/record-drafts", {"record_id": record_id, "base_revision_id": record["revision_id"], "payload": payload})
    exclusive_content = exclusive_png()
    exclusive_hash = hashlib.sha256(exclusive_content).hexdigest()
    _, exclusive = client.json(201, "POST", "/api/attachment-uploads", {"draft_id": edit["draft_id"], "display_name": "exclusive.png", "media_type": "image/png", "declared_size_bytes": len(exclusive_content)})
    client.expect(200, "PUT", "/api/attachment-uploads/" + exclusive["upload_id"] + "/content", exclusive_content, {"Content-Type": "application/octet-stream", "X-Houfeng-Draft-ID": edit["draft_id"], "X-Content-SHA256": exclusive_hash})
    client.json(202, "POST", "/api/attachment-uploads/" + exclusive["upload_id"] + "/complete", {"draft_id": edit["draft_id"]})
    deadline = time.monotonic() + 120
    while True:
        _, exclusive_metadata = client.json(200, "GET", "/api/attachments/" + exclusive["attachment_id"])
        if exclusive_metadata["state"] == "available": break
        if exclusive_metadata["state"] in ("rejected", "expired") or time.monotonic() >= deadline:
            raise SmokeError("exclusive attachment did not become available")
        time.sleep(0.5)
    if not exclusive_metadata.get("preview_available"):
        raise SmokeError("exclusive attachment preview is unavailable")
    _, exclusive_download = client.expect(200, "GET", "/api/attachments/" + exclusive["attachment_id"] + "/content")
    _, exclusive_preview = client.expect(200, "GET", "/api/attachments/" + exclusive["attachment_id"] + "/content?variant=preview")
    if exclusive_download != exclusive_content or not exclusive_preview or exclusive_preview == exclusive_content:
        raise SmokeError("exclusive attachment or distinct preview differs")
    payload["attachment_ids"] = [attachment_id, exclusive["attachment_id"]]
    _, edit = client.json(200, "PATCH", "/api/record-drafts/" + edit["draft_id"], {"payload": payload}, {"If-Match": edit["etag"]})
    _, revised = client.json(201, "POST", "/api/records/" + record_id + "/revisions", {"draft_id": edit["draft_id"], "draft_etag": edit["etag"], "base_revision_id": record["revision_id"], "lock_version": record["lock_version"], "authorization_epoch": record["authorization_epoch"]}, {"Idempotency-Key": "g2arc-smoke-exclusive-revision"})
    record = revised
    _, cleanup = client.json(200, "POST", "/api/records/client-cleanup/reconcile", {"browser_id": "g2arc_smoke", "record_ids": [record_id]})
    if record_id not in cleanup.get("allowed", []):
        raise SmokeError("cleanup target was not registered for the record")
    revoke = draft_handoff_revoke(client, sys.argv[2], source_id)
    return {"status": "PASS", "scope": "authenticated attachment, portability and draft ownership handoff segment only", "actor_id": actor["user_id"], "draft_id": draft_id, "upload_id": upload_id, "attachment_id": attachment_id, "record_id": record_id, "revision_id": record["revision_id"], "exclusive_attachment_id": exclusive["attachment_id"], "exclusive_content_sha256": exclusive_hash, "exclusive_preview_sha256": hashlib.sha256(exclusive_preview).hexdigest(), "imported_record_ids": applied["record_ids"], "imported_attachment_id": imported_ids[0], "content_sha256": sha, "content_bytes": len(content), "preview_sha256": hashlib.sha256(preview).hexdigest(), "preview_bytes": len(preview), "archive_sha256": hashlib.sha256(archive).hexdigest(), "archive_bytes": len(archive), "permanent_delete_routes": {"status": 503, "code": "deletion_safety_unavailable", "routes": ["permanent-delete-preview", "permanent-delete"]}, "draft_handoff_revoke": revoke, "http": client.events}

if __name__ == "__main__":
    try:
        print(json.dumps(run(), sort_keys=True))
    except (SmokeError, OSError, ValueError, KeyError, TypeError, urllib.error.URLError) as exc:
        print("attachment segment failed:", str(exc), file=sys.stderr)
        sys.exit(1)
