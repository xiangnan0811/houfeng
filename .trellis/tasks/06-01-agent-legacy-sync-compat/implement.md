# Implementation Plan

1. Add legacy token reader test in `agent/token/file_test.go`.
2. Extend `agent/token/file.go` with `node_id` read compatibility while keeping current writes unchanged.
3. Add legacy sync queue reader test in `agent/syncqueue/store_test.go`.
4. Extend sync queue JSON loading to map legacy `request.node_id` into `request.monitoring_instance_id`.
5. Add installer source test for preserving current and legacy post-enrollment token shapes.
6. Update `internal/center/installer/houfeng-agent-install.sh` preserve condition.
7. Run targeted tests:
   - `go test ./agent/token ./agent/syncqueue ./internal/center/installer`
8. Run `make verify-go`.

## Rollback

Revert the compatibility readers and installer condition. Current-format token and queue behavior should remain covered by existing tests.
