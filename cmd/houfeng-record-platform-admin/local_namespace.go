package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store"
)

var errLocalNamespaceRequest = errors.New("local namespace request is invalid; supply a private one-shot request outside BlobRoot")
var errLocalNamespaceOperation = errors.New("local namespace provisioning failed; keep writers stopped and resolve the original request before retrying")

type localNamespaceRequest struct {
	DatabaseURL   string `json:"database_url"`
	Root          string `json:"root"`
	NamespaceID   string `json:"namespace_id"`
	IncarnationID string `json:"incarnation_id"`
	RequestID     string `json:"request_id"`
	OwnerUID      uint32 `json:"owner_uid"`
	OwnerGID      uint32 `json:"owner_gid"`
}

// This one-shot command consumes its private credential input before opening
// the connection. Retry uses the same caller-preserved namespace/request IDs.
// Source-enabled adoption requires the separate authenticated outer handoff;
// the provisioner rejects it before changing the filesystem.
func runLocalNamespace(ctx context.Context, args []string) error {
	if len(args) != 4 || args[0] != "local-namespace" || args[1] != "provision" || args[2] != "--request-file" {
		return errLocalNamespaceRequest
	}
	path, err := filepath.Abs(args[3])
	if err != nil {
		return errLocalNamespaceRequest
	}
	identity, err := os.Lstat(path)
	if err != nil {
		return errLocalNamespaceRequest
	}
	body, err := readSourceEnrollmentRequest(path)
	if err != nil {
		return errLocalNamespaceRequest
	}
	defer clear(body)
	var request localNamespaceRequest
	schema := sourceRequestSchema{"database_url": nil, "root": nil, "namespace_id": nil, "incarnation_id": nil, "request_id": nil, "owner_uid": nil, "owner_gid": nil}
	if decodeSourceEnrollmentRequest(body, &request, schema) != nil || !filepath.IsAbs(request.Root) || filepath.Clean(request.Root) != request.Root {
		return errLocalNamespaceRequest
	}
	resolvedInput, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errLocalNamespaceRequest
	}
	relative, err := filepath.Rel(request.Root, resolvedInput)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))) {
		return errLocalNamespaceRequest
	}
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(request.DatabaseURL)
	request.DatabaseURL = ""
	if err != nil {
		return errLocalNamespaceRequest
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(identity, current) || !sourceEnrollmentPrivateFile(current) {
		return errLocalNamespaceRequest
	}
	if err := os.Remove(path); err != nil {
		return errLocalNamespaceRequest
	}
	clear(body)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errLocalNamespaceOperation
	}
	defer pool.Close()
	namespace, err := attachments.ProvisionLocalNamespace(ctx, attachments.LocalProvisionRequest{
		Root: request.Root, NamespaceID: request.NamespaceID, IncarnationID: request.IncarnationID, RequestID: request.RequestID,
		OwnerUID: request.OwnerUID, OwnerGID: request.OwnerGID,
	}, store.NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		return errLocalNamespaceOperation
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		NamespaceID    string `json:"namespace_id"`
		IncarnationID  string `json:"incarnation_id"`
		RequestID      string `json:"request_id"`
		AdmissionState string `json:"admission_state"`
	}{namespace.NamespaceID, namespace.IncarnationID, namespace.RequestID, namespace.AdmissionState}); err != nil {
		return errLocalNamespaceOperation
	}
	return nil
}
