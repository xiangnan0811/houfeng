package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"houfeng/internal/center/subscriptions"
)

func SubscriptionsCollection(repo subscriptions.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters, err := subscriptionFiltersFromQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			records, err := repo.ListSubscriptions(r.Context(), filters)
			if errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input subscriptions.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = subscriptions.NormalizeCreateInput(input)
			if err := subscriptions.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateSubscription(r.Context(), input)
			if errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func SubscriptionItem(repo subscriptions.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subscriptionID := strings.TrimPrefix(r.URL.Path, "/api/subscriptions/")
		subscriptionID = strings.Trim(subscriptionID, "/")
		if subscriptionID == "" || strings.Contains(subscriptionID, "/") {
			writeError(w, http.StatusNotFound, "subscription not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetSubscription(r.Context(), subscriptionID)
			if errors.Is(err, subscriptions.ErrSubscriptionNotFound) {
				writeError(w, http.StatusNotFound, "subscription not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		case http.MethodPatch:
			var input subscriptions.PatchInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = subscriptions.NormalizePatchInput(input)
			if err := subscriptions.ValidatePatchInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.PatchSubscription(r.Context(), subscriptionID, input)
			if errors.Is(err, subscriptions.ErrSubscriptionNotFound) {
				writeError(w, http.StatusNotFound, "subscription not found")
				return
			}
			if errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func subscriptionFiltersFromQuery(r *http.Request) (subscriptions.ListFilters, error) {
	query := r.URL.Query()
	filters := subscriptions.ListFilters{
		VPSID:  query.Get("vps_id"),
		Status: subscriptions.Status(query.Get("status")),
		Sort:   query.Get("sort"),
		Order:  query.Get("order"),
	}
	if raw := query.Get("renew_before"); raw != "" {
		date, err := subscriptions.ParseDate(raw)
		if err != nil {
			return subscriptions.ListFilters{}, err
		}
		filters.RenewBefore = &date
	}
	if raw := query.Get("renew_after"); raw != "" {
		date, err := subscriptions.ParseDate(raw)
		if err != nil {
			return subscriptions.ListFilters{}, err
		}
		filters.RenewAfter = &date
	}
	if raw := strings.TrimSpace(query.Get("renew_within_days")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return subscriptions.ListFilters{}, subscriptions.ErrInvalidSubscriptionInput
		}
		filters.RenewWithinDays = &days
	}

	filters = subscriptions.NormalizeListFilters(filters)
	if err := subscriptions.ValidateListFilters(filters); err != nil {
		return subscriptions.ListFilters{}, err
	}
	return filters, nil
}
