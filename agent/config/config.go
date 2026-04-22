package config

import (
	"fmt"
	"os"
	"strings"
)

type AgentConfig struct {
	ServerURL string
	TokenFile string
	NodeName  string
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

	nodeName, err := requiredEnv("HOUFENG_AGENT_NODE_NAME")
	if err != nil {
		return AgentConfig{}, err
	}

	return AgentConfig{
		ServerURL: serverURL,
		TokenFile: tokenFile,
		NodeName:  nodeName,
	}, nil
}

func requiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return value, nil
}
