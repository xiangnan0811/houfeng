package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	"houfeng/internal/center/deploy"
)

type legacyC81MigrationManagement struct {
	AppAdminURL         string                     `json:"app_admin_url"`
	AppPreviousAdminURL string                     `json:"app_previous_admin_url"`
	Fence               recordDeletionFenceRequest `json:"fence"`
}

var legacyC81MigrationSchema = sourceRequestSchema{
	"app_admin_url": nil, "app_previous_admin_url": nil,
	"fence": recordDeletionRequestSchema["fence"],
}

type legacyC81MigrationResponse struct {
	Version        int    `json:"version"`
	Status         string `json:"status"`
	ManifestDigest string `json:"manifest_digest"`
}

type legacyC81HandledResultError struct{}

func (*legacyC81HandledResultError) Error() string {
	return "legacy C81 migration returned a controlled failure"
}

func runLegacyC81Migration(ctx context.Context, args []string) error {
	return runLegacyC81MigrationWith(ctx, args, os.Stdout, deploy.PrepareLegacyC81MigrationCoordinator, deploy.RunLegacyC81MigrationMaintenance)
}

func runLegacyC81MigrationWith(ctx context.Context, args []string, output io.Writer, prepare func() error, maintenance func(context.Context, deploy.RecordLedgerMaintenanceRequest) (deploy.LegacyC81MigrationResult, error)) error {
	if output == nil {
		return errRecordDeletionAdmin
	}
	write := func(result legacyC81MigrationResponse, failed bool) error {
		body, err := json.Marshal(result)
		if err != nil {
			return errRecordDeletionAdmin
		}
		body = append(body, '\n')
		if n, err := output.Write(body); err != nil || n != len(body) {
			return errRecordDeletionAdmin
		}
		if failed {
			return &legacyC81HandledResultError{}
		}
		return nil
	}
	unavailable := func() error {
		return write(legacyC81MigrationResponse{Version: 1, Status: "unavailable"}, true)
	}
	if ctx == nil || prepare == nil || maintenance == nil || len(args) != 4 || args[0] != "record-deletion" ||
		args[1] != "legacy-c81-migrate" || args[2] != "--request-file" || args[3] == "" || prepare() != nil {
		return unavailable()
	}
	body, err := readSourceEnrollmentRequest(args[3])
	if err != nil {
		return unavailable()
	}
	defer clear(body)
	var input legacyC81MigrationManagement
	if decodeLegacySourceJSON(body, &input, legacyC81MigrationSchema) != nil ||
		!legacySourceMaintenanceValid(input.AppAdminURL, input.AppPreviousAdminURL, input.Fence) {
		return unavailable()
	}
	result, err := maintenance(ctx, recordLedgerMaintenance(input.AppAdminURL, input.AppPreviousAdminURL, input.Fence))
	raw, decodeErr := hex.DecodeString(result.ManifestDigest)
	if err != nil || decodeErr != nil || len(raw) != 32 || hex.EncodeToString(raw) != result.ManifestDigest {
		return unavailable()
	}
	var nonzero bool
	for _, b := range raw {
		nonzero = nonzero || b != 0
	}
	if !nonzero {
		return unavailable()
	}
	status := "converged"
	if result.Recovered {
		status = "recovered_converged"
	}
	return write(legacyC81MigrationResponse{Version: 1, Status: status, ManifestDigest: result.ManifestDigest}, false)
}
