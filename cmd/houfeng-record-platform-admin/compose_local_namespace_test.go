package main

import (
	"context"
	"errors"
	"testing"
)

func TestComposeLocalNamespaceDispatchAndGuard(t *testing.T) {
	for _, backend := range []string{"local", "s3"} {
		t.Run(backend, func(t *testing.T) {
			guarded := false
			calls := 0
			deps := appAdminDependencies{namespace: composeLocalNamespaceDependencies{
				getenv: func(key string) string {
					if key != "HOUFENG_ATTACHMENT_BLOB_BACKEND" {
						t.Fatalf("unexpected environment key %q", key)
					}
					return backend
				},
				guard: func(ctx context.Context, root string, exclusive bool, fn func() error) error {
					if ctx != t.Context() || root != "" || exclusive {
						t.Fatalf("unexpected guard arguments: root=%q exclusive=%v", root, exclusive)
					}
					guarded = true
					defer func() { guarded = false }()
					return fn()
				},
				initialize: func(ctx context.Context, got string) error {
					if !guarded || ctx != t.Context() || got != backend {
						t.Fatal("initializer ran outside the shared guard or received wrong configuration")
					}
					calls++
					return nil
				},
			}}
			if err := runAdminWithDeps(t.Context(), []string{"deploy-local-namespace", "--scope", "compose"}, deps); err != nil || calls != 1 {
				t.Fatalf("namespace dispatch: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestComposeLocalNamespaceRejectsInvalidInputBeforeGuard(t *testing.T) {
	calls := 0
	deps := appAdminDependencies{namespace: composeLocalNamespaceDependencies{
		getenv:     func(string) string { return "local" },
		guard:      func(context.Context, string, bool, func() error) error { calls++; return nil },
		initialize: func(context.Context, string) error { calls++; return nil },
	}}
	for _, args := range [][]string{
		{"deploy-local-namespace"},
		{"deploy-local-namespace", "--scope", "app"},
		{"deploy-local-namespace", "--scope", "compose", "extra"},
		{"deploy-local-namespace", "--scope", "compose", "--root", "/tmp"},
	} {
		if err := runAdminWithDeps(t.Context(), args, deps); !errors.Is(err, errComposeLocalNamespaceInvocation) || calls != 0 {
			t.Fatalf("args %q: err=%v calls=%d", args, err, calls)
		}
	}
	deps.namespace.getenv = func(string) string { return "unknown" }
	if err := runAdminWithDeps(t.Context(), []string{"deploy-local-namespace", "--scope", "compose"}, deps); err == nil || calls != 0 {
		t.Fatalf("invalid backend: err=%v calls=%d", err, calls)
	}
}

func TestComposeLocalNamespacePropagatesGuardFailure(t *testing.T) {
	failure := errors.New("guard refused")
	deps := appAdminDependencies{namespace: composeLocalNamespaceDependencies{
		getenv:     func(string) string { return "local" },
		guard:      func(context.Context, string, bool, func() error) error { return failure },
		initialize: func(context.Context, string) error { t.Fatal("initializer called without guard"); return nil },
	}}
	if err := runAdminWithDeps(t.Context(), []string{"deploy-local-namespace", "--scope", "compose"}, deps); !errors.Is(err, failure) {
		t.Fatalf("guard failure = %v", err)
	}
}

func TestComposeLocalNamespacePropagatesInitializationFailure(t *testing.T) {
	failure := errors.New("namespace refused")
	deps := appAdminDependencies{namespace: composeLocalNamespaceDependencies{
		getenv:     func(string) string { return "local" },
		guard:      func(_ context.Context, _ string, _ bool, fn func() error) error { return fn() },
		initialize: func(context.Context, string) error { return failure },
	}}
	if err := runAdminWithDeps(t.Context(), []string{"deploy-local-namespace", "--scope", "compose"}, deps); !errors.Is(err, failure) {
		t.Fatalf("initializer failure = %v", err)
	}
}
