package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	defaultHTTPAddr   = ":8080"
	defaultWebDistDir = "web/dist"
)

type CenterConfig struct {
	HTTPAddr    string
	WebDistDir  string
	DatabaseURL string
}

func LoadCenterConfig() (CenterConfig, error) {
	httpAddr, err := envOrDefault("HOUFENG_HTTP_ADDR", defaultHTTPAddr)
	if err != nil {
		return CenterConfig{}, err
	}

	webDistDir, err := envOrDefault("HOUFENG_WEB_DIST_DIR", defaultWebDistDir)
	if err != nil {
		return CenterConfig{}, err
	}

	databaseURL, err := requiredEnv("HOUFENG_DATABASE_URL")
	if err != nil {
		return CenterConfig{}, err
	}

	return CenterConfig{
		HTTPAddr:    httpAddr,
		WebDistDir:  webDistDir,
		DatabaseURL: databaseURL,
	}, nil
}

func envOrDefault(key, fallback string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		value = fallback
	}
	return nonEmptyEnvValue(key, value)
}

func requiredEnv(key string) (string, error) {
	return nonEmptyEnvValue(key, os.Getenv(key))
}

func nonEmptyEnvValue(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}

	return value, nil
}
