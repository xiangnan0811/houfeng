package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

type vpsSubscriptionCreateRequest struct {
	Price              float64             `json:"price"`
	Currency           string              `json:"currency"`
	BillingCycle       string              `json:"billing_cycle"`
	BillingMonths      int                 `json:"billing_months"`
	StartedAt          *subscriptions.Date `json:"started_at"`
	RenewAt            *subscriptions.Date `json:"renew_at"`
	AutoRenew          bool                `json:"auto_renew"`
	AutoRenewCancelled bool                `json:"auto_renew_cancelled"`
	PaymentMethod      string              `json:"payment_method"`
	Note               string              `json:"note"`
}

func SubscriptionsCollection(repo subscriptions.Repository, costSvc ...*subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters, err := subscriptionFiltersFromQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			effectiveFilters := filters
			effectiveFilters.BudgetStatus = ""
			records, err := repo.ListSubscriptions(r.Context(), effectiveFilters)
			if errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if len(costSvc) > 0 && costSvc[0] != nil {
				costRows, err := costSvc[0].ListCostRows(r.Context())
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				records = mergeSubscriptionCosts(records, costRows, filters.BudgetStatus)
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

func mergeSubscriptionCosts(records []subscriptions.Record, costs []subscriptioncosts.CostRow, budgetStatus string) []subscriptions.Record {
	byID := make(map[string]subscriptioncosts.CostRow, len(costs))
	for _, cost := range costs {
		byID[cost.SubscriptionID] = cost
	}
	merged := make([]subscriptions.Record, 0, len(records))
	for _, record := range records {
		if cost, ok := byID[record.SubscriptionID]; ok {
			record.MonthlyPriceBase = cost.MonthlyPriceBase
			record.YearlyPriceBase = cost.YearlyPriceBase
			record.BaseCurrency = cost.BaseCurrency
			record.ExchangeRate = cost.ExchangeRate
			record.ExchangeRateDate = cost.ExchangeRateDate
			record.ExchangeRateStale = cost.ExchangeRateStale
			record.BudgetStatus = string(cost.BudgetStatus)
			record.NextReminderAt = cost.NextReminderAt
		}
		if budgetStatus != "" && record.BudgetStatus != budgetStatus {
			continue
		}
		merged = append(merged, record)
	}
	return merged
}

func VPSSubscriptions(repo subscriptions.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "subscriptions")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			filters, err := subscriptionFiltersFromQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			filters.VPSID = vpsID

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
			var request vpsSubscriptionCreateRequest
			if err := decodeJSON(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input := subscriptions.CreateInput{
				VPSID:              vpsID,
				Price:              request.Price,
				Currency:           request.Currency,
				BillingCycle:       request.BillingCycle,
				BillingMonths:      request.BillingMonths,
				StartedAt:          request.StartedAt,
				RenewAt:            request.RenewAt,
				AutoRenew:          request.AutoRenew,
				AutoRenewCancelled: request.AutoRenewCancelled,
				Status:             subscriptions.DefaultStatus,
				PaymentMethod:      request.PaymentMethod,
				Note:               request.Note,
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
	if raw := strings.TrimSpace(query.Get("currency")); raw != "" {
		filters.Currency = raw
	}
	if raw := strings.TrimSpace(query.Get("provider_id")); raw != "" {
		filters.ProviderID = raw
	}
	if raw := strings.TrimSpace(query.Get("budget_status")); raw != "" {
		filters.BudgetStatus = raw
	}
	if raw := strings.TrimSpace(query.Get("auto_renew")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return subscriptions.ListFilters{}, subscriptions.ErrInvalidSubscriptionInput
		}
		filters.AutoRenew = &value
	}
	if raw := strings.TrimSpace(query.Get("payment_method")); raw != "" {
		filters.PaymentMethod = raw
	}
	if raw := strings.TrimSpace(query.Get("label")); raw != "" {
		filters.Label = raw
	}
	if raw := strings.TrimSpace(query.Get("renewal_decision")); raw != "" {
		filters.RenewalDecision = raw
	}
	if raw := strings.TrimSpace(query.Get("asset_scope")); raw != "" {
		filters.AssetScope = vpsassets.AssetScope(raw)
	}

	filters = subscriptions.NormalizeListFilters(filters)
	if filters.AssetScope == "" {
		filters.AssetScope = vpsassets.AssetScopeCurrent
	}
	if err := subscriptions.ValidateListFilters(filters); err != nil {
		return subscriptions.ListFilters{}, err
	}
	return filters, nil
}
