//go:build !linux

package deploy

import (
	"context"
	"errors"
)

var ErrComposeLocalNamespace = errors.New("Compose local namespace initialization requires Linux")

func InitializeComposeLocalNamespace(context.Context, string) error {
	return ErrComposeLocalNamespace
}
