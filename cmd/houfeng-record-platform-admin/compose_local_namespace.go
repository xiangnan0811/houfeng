package main

import (
	"context"
	"errors"
)

var errComposeLocalNamespaceInvocation = errors.New("only deploy-local-namespace --scope compose is supported")

type composeLocalNamespaceDependencies struct {
	guard      func(context.Context, string, bool, func() error) error
	initialize func(context.Context, string) error
	getenv     func(string) string
}

// The caller must stop external writers and restart controllers before running
// namespace initialization; the shared guard only coordinates managers.
func runComposeLocalNamespace(ctx context.Context, args []string, deps composeLocalNamespaceDependencies) error {
	if len(args) != 3 || args[0] != "deploy-local-namespace" || args[1] != "--scope" || args[2] != "compose" {
		return errComposeLocalNamespaceInvocation
	}
	if deps.guard == nil || deps.initialize == nil || deps.getenv == nil {
		return errors.New("Compose local namespace dependencies unavailable")
	}
	backend := deps.getenv("HOUFENG_ATTACHMENT_BLOB_BACKEND")
	if backend != "local" && backend != "s3" {
		return errors.New("invalid Compose attachment blob backend")
	}
	return deps.guard(ctx, "", false, func() error {
		return deps.initialize(ctx, backend)
	})
}
