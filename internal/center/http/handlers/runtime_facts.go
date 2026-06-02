package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

type hostSampleSubscriber interface {
	SubscribeHostSamples(string) runtimefacts.HostSampleSubscription
}

type monitoringInstanceGetter interface {
	GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error)
}

func MonitoringInstanceRuntimeFacts(repo runtimefacts.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceRuntimeFactsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		window, err := parseMonitoringRuntimeWindow(r.URL.Query().Get("window"), time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window: "+err.Error())
			return
		}

		record, err := repo.GetMonitoringInstanceRuntimeFacts(r.Context(), monitoringInstanceID, window)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func TargetRuntimeFacts(repo runtimefacts.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		targetID, ok := targetRuntimeFactsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		window, limit, err := parseWindow(r.URL.Query().Get("window"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window: "+err.Error())
			return
		}
		since := time.Now().Add(-window)

		record, err := repo.GetTargetRuntimeFacts(r.Context(), targetID, since, limit)
		if errors.Is(err, targets.ErrTargetNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func MonitoringInstanceRuntimeStream(repo monitoringInstanceGetter, hub hostSampleSubscriber) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceRuntimeStreamPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if repo == nil || hub == nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if _, err := repo.GetMonitoringInstance(r.Context(), monitoringInstanceID); errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := conn.CloseRead(r.Context())

		subscription := hub.SubscribeHostSamples(monitoringInstanceID)
		defer subscription.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-subscription.Messages:
				if !ok {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := wsjson.Write(writeCtx, conn, message)
				cancel()
				if err != nil {
					return
				}
			}
		}
	})
}

func monitoringInstanceRuntimeFactsPath(path string) (monitoringInstanceID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-facts" {
		return "", false
	}
	return segments[0], true
}

func monitoringInstanceRuntimeStreamPath(path string) (monitoringInstanceID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-stream" {
		return "", false
	}
	return segments[0], true
}

func parseMonitoringRuntimeWindow(raw string, now time.Time) (runtimefacts.WindowRequest, error) {
	key := strings.TrimSpace(raw)
	var duration time.Duration
	var bucketCount int
	switch key {
	case "", "24h":
		key = "24h"
		duration = 24 * time.Hour
		bucketCount = 288
	case "7d":
		duration = 7 * 24 * time.Hour
		bucketCount = 336
	case "30d":
		duration = 30 * 24 * time.Hour
		bucketCount = 720
	default:
		return runtimefacts.WindowRequest{}, errMetric("must be 24h, 7d, or 30d")
	}

	now = now.UTC()
	return runtimefacts.WindowRequest{
		Key:         key,
		StartedAt:   now.Add(-duration),
		EndedAt:     now,
		BucketCount: bucketCount,
	}, nil
}

func targetRuntimeFactsPath(path string) (targetID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-facts" {
		return "", false
	}
	return segments[0], true
}
