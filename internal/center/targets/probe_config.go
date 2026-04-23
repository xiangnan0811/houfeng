package targets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var errInvalidProbeItemInput = errors.New("invalid probe item input")

type TCPProbeConfig struct {
	Port int `json:"port"`
}

type HTTPProbeConfig struct {
	Scheme              string `json:"scheme"`
	Path                string `json:"path"`
	Method              string `json:"method"`
	ExpectedStatusRange []int  `json:"expected_status_range"`
}

type TLSProbeConfig struct {
	Port              int `json:"port"`
	ExpiryWarningDays int `json:"expiry_warning_days"`
}

func ValidateCreateProbeItemInput(input CreateProbeItemInput) (CreateProbeItemInput, error) {
	input.ProbeKind = strings.TrimSpace(input.ProbeKind)
	input.FrequencyTier = strings.TrimSpace(input.FrequencyTier)

	if !IsValidProbeKind(input.ProbeKind) || !IsValidFrequencyTier(input.FrequencyTier) || input.TimeoutSeconds <= 0 {
		return CreateProbeItemInput{}, errInvalidProbeItemInput
	}

	config, err := normalizeProbeConfig(input.ProbeKind, input.Config)
	if err != nil {
		return CreateProbeItemInput{}, fmt.Errorf("%w: %w", errInvalidProbeItemInput, err)
	}

	input.Config = config
	return input, nil
}

func normalizeProbeConfig(probeKind string, raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("config is required")
	}

	switch probeKind {
	case ProbeKindTCP:
		var config TCPProbeConfig
		if err := decodeStrictJSON(raw, &config); err != nil {
			return nil, err
		}
		if config.Port <= 0 {
			return nil, errors.New("tcp port must be positive")
		}
		return json.Marshal(config)
	case ProbeKindHTTP:
		var config HTTPProbeConfig
		if err := decodeStrictJSON(raw, &config); err != nil {
			return nil, err
		}
		config.Scheme = strings.TrimSpace(config.Scheme)
		config.Path = strings.TrimSpace(config.Path)
		config.Method = strings.ToUpper(strings.TrimSpace(config.Method))

		if config.Scheme == "" || config.Path == "" || config.Method == "" || len(config.ExpectedStatusRange) == 0 {
			return nil, errors.New("http config fields are required")
		}
		if config.Method != "GET" && config.Method != "HEAD" {
			return nil, errors.New("http method must be GET or HEAD")
		}
		if len(config.ExpectedStatusRange) != 2 {
			return nil, errors.New("expected_status_range must contain exactly two values")
		}
		low, high := config.ExpectedStatusRange[0], config.ExpectedStatusRange[1]
		if low <= 0 || high <= 0 || low > high {
			return nil, errors.New("expected_status_range must be positive and ordered")
		}
		return json.Marshal(config)
	case ProbeKindTLS:
		var config TLSProbeConfig
		if err := decodeStrictJSON(raw, &config); err != nil {
			return nil, err
		}
		if config.Port <= 0 || config.ExpiryWarningDays <= 0 {
			return nil, errors.New("tls config values must be positive")
		}
		return json.Marshal(config)
	default:
		return nil, errors.New("unsupported probe kind")
	}
}

func decodeStrictJSON(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing data")
		}
		return err
	}
	return nil
}
