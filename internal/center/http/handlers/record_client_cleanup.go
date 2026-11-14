package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordclientcleanup"
)

func RecordClientCleanupReconcile(repository recordclientcleanup.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		actor, ok := sessionctx.ActorScopeFromContext(r.Context())
		if !ok || repository == nil {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		var request recordclientcleanup.ReconcileRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeRecordError(w, http.StatusBadRequest, "cleanup_invalid", "invalid cleanup request", nil)
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeRecordError(w, http.StatusBadRequest, "cleanup_invalid", "invalid cleanup request", nil)
			return
		}
		if err := request.Validate(); err != nil {
			writeRecordError(w, http.StatusBadRequest, "cleanup_invalid", "invalid cleanup request", nil)
			return
		}
		result, err := repository.Reconcile(r.Context(), actor, request)
		if err != nil {
			writeRecordsApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

type recordCleanupClientMessage struct {
	Type string                              `json:"type"`
	Ack  recordclientcleanup.Acknowledgement `json:"ack"`
}

type recordCleanupServerMessage struct {
	Type  string                     `json:"type"`
	Event *recordclientcleanup.Event `json:"event,omitempty"`
}

func RecordClientCleanupStream(repository recordclientcleanup.Repository, validator sessionValidator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		actor, actorOK := sessionctx.ActorScopeFromContext(r.Context())
		sessionID, sessionOK := sessionctx.SessionIDFromContext(r.Context())
		if !sessionOK || sessionID == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if !actorOK || repository == nil || validator == nil {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		query := r.URL.Query()
		browserID, cursor := query.Get("browser_id"), query.Get("cursor")
		if len(query["browser_id"]) != 1 || len(query["cursor"]) > 1 || recordclientcleanup.ValidateBrowserID(browserID) != nil {
			writeError(w, http.StatusBadRequest, "invalid cleanup identity")
			return
		}
		for key := range query {
			if key != "browser_id" && key != "cursor" {
				writeError(w, http.StatusBadRequest, "invalid cleanup query")
				return
			}
		}
		checkSession := func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, runtimeStreamAuthorityTimeout)
			defer cancel()
			return validator.ValidateSession(checkCtx, sessionID)
		}
		if err := checkSession(r.Context()); err != nil {
			if errors.Is(err, auth.ErrSessionNotFound) || errors.Is(err, auth.ErrSessionExpired) {
				writeError(w, http.StatusUnauthorized, "unauthenticated")
			} else {
				writeRecordInternalError(w)
			}
			return
		}
		checkCtx, checkCancel := context.WithTimeout(r.Context(), runtimeStreamAuthorityTimeout)
		cursorErr := repository.ValidateCursor(checkCtx, actor, browserID, cursor)
		checkCancel()
		if cursorErr != nil && !errors.Is(cursorErr, recordclientcleanup.ErrReconcileRequired) {
			if errors.Is(cursorErr, recordclientcleanup.ErrInvalidRequest) {
				writeError(w, http.StatusBadRequest, "invalid cleanup cursor")
			} else {
				writeRecordsApplicationError(w, cursorErr)
			}
			return
		}
		// Keep coder/websocket's same-origin policy; never permit arbitrary origins.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		defer conn.CloseNow()
		conn.SetReadLimit(4096)
		writeMessage := func(message recordCleanupServerMessage) error {
			if err := checkSession(ctx); err != nil {
				return err
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, runtimeStreamWriteTimeout)
			defer writeCancel()
			return wsjson.Write(writeCtx, conn, message)
		}
		if errors.Is(cursorErr, recordclientcleanup.ErrReconcileRequired) {
			_ = writeMessage(recordCleanupServerMessage{Type: "reconcile_required"})
			return
		}
		acks := make(chan recordclientcleanup.Acknowledgement)
		readErrors := make(chan error, 1)
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			for {
				kind, data, err := conn.Read(ctx)
				if err != nil {
					readErrors <- err
					return
				}
				var message recordCleanupClientMessage
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.DisallowUnknownFields()
				err = decoder.Decode(&message)
				if err == nil {
					if trailing := decoder.Decode(&struct{}{}); !errors.Is(trailing, io.EOF) {
						err = recordclientcleanup.ErrInvalidAcknowledgement
					}
				}
				if kind != websocket.MessageText || err != nil || message.Type != "ack" || message.Ack.Validate() != nil {
					readErrors <- recordclientcleanup.ErrInvalidAcknowledgement
					return
				}
				select {
				case acks <- message.Ack:
				case <-ctx.Done():
					return
				}
			}
		}()
		watcherDone := make(chan struct{})
		go func() {
			defer close(watcherDone)
			ticker := time.NewTicker(runtimeStreamSessionCheckInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if checkSession(ctx) != nil {
						cancel()
						conn.CloseNow()
						return
					}
				}
			}
		}()
		defer func() { cancel(); conn.CloseNow(); <-readerDone; <-watcherDone }()
		ticker := time.NewTicker(runtimeStreamSessionCheckInterval)
		defer ticker.Stop()
		var pending *recordclientcleanup.Event
		var sentAt time.Time
		sendNext := func() error {
			readCtx, readCancel := context.WithTimeout(ctx, runtimeStreamAuthorityTimeout)
			defer readCancel()
			next, err := repository.Next(readCtx, actor, browserID)
			if err != nil {
				return err
			}
			if next == nil {
				return nil
			}
			if err := next.Validate(); err != nil {
				return err
			}
			if err := writeMessage(recordCleanupServerMessage{Type: "cleanup", Event: next}); err != nil {
				return err
			}
			pending = next
			sentAt = time.Now()
			return nil
		}
		if err := sendNext(); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-readErrors:
				return
			case ack := <-acks:
				if pending == nil || ack != pending.Acknowledgement() || checkSession(ctx) != nil {
					return
				}
				ackCtx, ackCancel := context.WithTimeout(ctx, runtimeStreamAuthorityTimeout)
				err := repository.Acknowledge(ackCtx, actor, browserID, ack)
				ackCancel()
				if err != nil {
					return
				}
				pending = nil
				if err := sendNext(); err != nil {
					return
				}
			case <-ticker.C:
				if pending != nil {
					// A non-cooperating tab is not an acknowledgement. Reconnection
					// replays this durable event rather than advancing the stream.
					if time.Since(sentAt) >= 30*time.Second {
						return
					}
					continue
				}
				if err := sendNext(); err != nil {
					return
				}
			}
		}
	})
}
