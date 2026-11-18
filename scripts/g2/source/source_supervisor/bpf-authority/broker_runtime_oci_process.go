//go:build linux && amd64 && !cgo

package main

import (
	"reflect"
	"strings"
)

func runtimeObject(value any) (map[string]any, error) {
	item, ok := value.(map[string]any)
	if !ok {
		return nil, errGuardProtocol
	}
	return item, nil
}
func runtimeStrings(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errGuardProtocol
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.ContainsRune(text, 0) {
			return nil, errGuardProtocol
		}
		out = append(out, text)
	}
	return out, nil
}

func runtimeProcess(spec map[string]any, grant *brokerGrant) error {
	expected, err := expectedRuntimeSpec(nil, grant)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(spec["process"], expected["process"]) {
		return errGuardProtocol
	}
	return nil
}

func runtimeNamespaces(linux map[string]any, grant *brokerGrant) error {
	expected, err := expectedRuntimeSpec(nil, grant)
	if err != nil {
		return err
	}
	expectedLinux, ok := expected["linux"].(map[string]any)
	if !ok || !reflect.DeepEqual(linux["namespaces"], expectedLinux["namespaces"]) {
		return errGuardProtocol
	}
	return nil
}
