package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBufferFile       = "/var/lib/houfeng-agent/sync-buffer.json"
	DefaultBufferMaxEntries = 65536
	DefaultBufferMaxAge     = 72 * time.Hour
)

type AgentConfig struct {
	ServerURL        string
	TokenFile        string
	BufferFile       string
	BufferMaxEntries int
	BufferMaxAge     time.Duration
}

func LoadAgentConfig() (AgentConfig, error) {
	serverURL, err := requiredEnv("HOUFENG_AGENT_SERVER_URL")
	if err != nil {
		return AgentConfig{}, err
	}

	tokenFile, err := requiredEnv("HOUFENG_AGENT_TOKEN_FILE")
	if err != nil {
		return AgentConfig{}, err
	}

	bufferMaxEntries, err := optionalPositiveIntEnv("HOUFENG_AGENT_BUFFER_MAX_ENTRIES", DefaultBufferMaxEntries)
	if err != nil {
		return AgentConfig{}, err
	}

	bufferMaxAge, err := optionalDurationEnv("HOUFENG_AGENT_BUFFER_MAX_AGE", DefaultBufferMaxAge)
	if err != nil {
		return AgentConfig{}, err
	}

	return AgentConfig{
		ServerURL:        serverURL,
		TokenFile:        tokenFile,
		BufferFile:       optionalEnv("HOUFENG_AGENT_BUFFER_FILE", DefaultBufferFile),
		BufferMaxEntries: bufferMaxEntries,
		BufferMaxAge:     bufferMaxAge,
	}, nil
}

func requiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return value, nil
}

func optionalEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func optionalPositiveIntEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a positive integer: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func optionalDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}
