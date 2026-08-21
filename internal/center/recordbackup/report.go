package recordbackup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const ReportFormatV1 = "houfeng-record-profile-report/v1"

type ProfileReportInput struct {
	Profile         Profile
	Commit          string
	ConfigDigest    [sha256.Size]byte
	Suites          []string
	PermanentDelete string
	Missing         []string
}

type ProfileReport struct {
	profile         Profile
	commit          string
	configDigest    [sha256.Size]byte
	suites          []string
	permanentDelete string
	missing         []string
}

func NewProfileReport(input ProfileReportInput) (ProfileReport, error) {
	if (input.Profile != ProfileLocal && input.Profile != ProfileS3) ||
		input.Commit == "" ||
		input.ConfigDigest == ([sha256.Size]byte{}) ||
		input.PermanentDelete == "" ||
		len(input.Suites) == 0 {
		return ProfileReport{}, ErrInvalidBackupRequest
	}
	suites := append([]string(nil), input.Suites...)
	missing := append([]string(nil), input.Missing...)
	sort.Strings(suites)
	sort.Strings(missing)
	return ProfileReport{
		profile:         input.Profile,
		commit:          input.Commit,
		configDigest:    input.ConfigDigest,
		suites:          suites,
		permanentDelete: input.PermanentDelete,
		missing:         missing,
	}, nil
}

func (report ProfileReport) Encode() ([]byte, error) {
	payload := encodedProfileReport{
		Format:          ReportFormatV1,
		Profile:         report.profile,
		Commit:          report.commit,
		ConfigDigest:    hex.EncodeToString(report.configDigest[:]),
		Suites:          append([]string(nil), report.suites...),
		PermanentDelete: report.permanentDelete,
		Missing:         append([]string(nil), report.missing...),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: report", ErrInvalidBackupRequest)
	}
	return encoded, nil
}

type encodedProfileReport struct {
	Format          string   `json:"format"`
	Profile         Profile  `json:"profile"`
	Commit          string   `json:"commit"`
	ConfigDigest    string   `json:"config_digest"`
	Suites          []string `json:"suites"`
	PermanentDelete string   `json:"permanent_delete"`
	Missing         []string `json:"missing"`
}
