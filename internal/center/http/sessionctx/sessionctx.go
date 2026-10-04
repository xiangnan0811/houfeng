package sessionctx

import (
	"context"

	"houfeng/internal/center/recordauth"
)

type contextKey int

const (
	userIDKey contextKey = iota
	actorScopeKey
	sessionIDKey
)

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// WithSessionID stores the server-authenticated bearer ID for trusted
// connection-local authorization checks.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// SessionIDFromContext returns the server-authenticated bearer ID.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(sessionIDKey).(string)
	return v, ok
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok
}

// WithActorScope stores a defensive actor copy for request-local consumers.
func WithActorScope(ctx context.Context, actor recordauth.ActorScope) context.Context {
	return context.WithValue(ctx, actorScopeKey, actor.Clone())
}

// ActorScopeFromContext returns a defensive copy of the typed trusted actor.
func ActorScopeFromContext(ctx context.Context) (recordauth.ActorScope, bool) {
	actor, ok := ctx.Value(actorScopeKey).(recordauth.ActorScope)
	if !ok {
		return recordauth.ActorScope{}, false
	}
	return actor.Clone(), true
}
