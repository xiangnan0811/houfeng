package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/subscriptions"
)

func SubscriptionOverview(service *subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		record, err := service.GetOverview(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func SubscriptionStatistics(service *subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		record, err := service.GetStatistics(r.Context(), r.URL.Query().Get("window"))
		if errors.Is(err, subscriptioncosts.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func SubscriptionSettings(service *subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			record, err := service.GetSettings(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, newSubscriptionCostSettingsResponse(record))
		case http.MethodPut:
			current, err := service.GetSettings(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			var input subscriptionCostSettingsUpdateRequest
			if err := decodeSettingsJSONBody(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			updatedInput := mergeSubscriptionCostSettingsUpdate(current, input)
			record, err := service.PutSettings(r.Context(), updatedInput)
			if errors.Is(err, centersettings.ErrInvalidSettings) || errors.Is(subscriptioncosts.MapSettingsError(err), subscriptioncosts.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, newSubscriptionCostSettingsResponse(record))
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func SubscriptionExchangeRateRefresh(service *subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		record, err := service.RefreshExchangeRates(r.Context())
		if errors.Is(err, subscriptioncosts.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func SubscriptionBudgets(service *subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters, err := budgetFiltersFromQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			records, err := service.ListBudgets(r.Context(), filters)
			if errors.Is(err, subscriptioncosts.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input subscriptioncosts.CreateBudgetInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			record, err := service.CreateBudget(r.Context(), input)
			if errors.Is(err, subscriptioncosts.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		case http.MethodPatch:
			var request budgetPatchRequest
			if err := decodeJSON(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			record, err := service.PatchBudget(r.Context(), request.toInput())
			if errors.Is(err, subscriptioncosts.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, subscriptioncosts.ErrBudgetNotFound) {
				writeError(w, http.StatusNotFound, "budget not found")
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

func SubscriptionMonthlyBudgets(service *subscriptioncosts.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := service.ListMonthlyBudgets(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPut:
			var input subscriptioncosts.UpsertMonthlyBudgetInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			month, ok := subscriptionMonthlyBudgetPathMonth(r.URL.Path)
			if !ok {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			if month != "" {
				parsed, err := subscriptions.ParseDate(month + "-01")
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid input")
					return
				}
				input.BudgetMonth = parsed
			}
			record, err := service.UpsertMonthlyBudget(r.Context(), input)
			if errors.Is(err, subscriptioncosts.ErrInvalidInput) {
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

func subscriptionMonthlyBudgetPathMonth(path string) (string, bool) {
	const prefix = "/api/subscription-monthly-budgets/"
	if !strings.HasPrefix(path, prefix) {
		return "", true
	}
	month := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if month == "" || strings.Contains(month, "/") {
		return "", false
	}
	return month, true
}

type budgetPatchRequest struct {
	BudgetID     string                 `json:"budget_id"`
	ScopeType    *string                `json:"scope_type,omitempty"`
	ScopeID      *string                `json:"scope_id,omitempty"`
	Name         *string                `json:"name,omitempty"`
	BaseCurrency *string                `json:"base_currency,omitempty"`
	MonthlyLimit patchNullableFloatJSON `json:"monthly_limit,omitempty"`
	YearlyLimit  patchNullableFloatJSON `json:"yearly_limit,omitempty"`
	WarningPct   *int                   `json:"warning_pct,omitempty"`
	Enabled      *bool                  `json:"enabled,omitempty"`
	Note         *string                `json:"note,omitempty"`
}

func (r budgetPatchRequest) toInput() subscriptioncosts.PatchBudgetInput {
	input := subscriptioncosts.PatchBudgetInput{BudgetID: r.BudgetID}
	if r.ScopeType != nil {
		input.ScopeType = subscriptioncosts.PatchString(*r.ScopeType)
	}
	if r.ScopeID != nil {
		input.ScopeID = subscriptioncosts.PatchString(*r.ScopeID)
	}
	if r.Name != nil {
		input.Name = subscriptioncosts.PatchString(*r.Name)
	}
	if r.BaseCurrency != nil {
		input.BaseCurrency = subscriptioncosts.PatchString(*r.BaseCurrency)
	}
	if r.MonthlyLimit.Set {
		input.MonthlyLimit = subscriptioncosts.PatchNullableFloat(r.MonthlyLimit.Value)
	}
	if r.YearlyLimit.Set {
		input.YearlyLimit = subscriptioncosts.PatchNullableFloat(r.YearlyLimit.Value)
	}
	if r.WarningPct != nil {
		input.WarningPct = subscriptioncosts.PatchInt(*r.WarningPct)
	}
	if r.Enabled != nil {
		input.Enabled = subscriptioncosts.PatchBool(*r.Enabled)
	}
	if r.Note != nil {
		input.Note = subscriptioncosts.PatchString(*r.Note)
	}
	return input
}

type patchNullableFloatJSON struct {
	Set   bool
	Value *float64
}

func (v *patchNullableFloatJSON) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		v.Value = nil
		return nil
	}
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = &value
	return nil
}

func budgetFiltersFromQuery(r *http.Request) (subscriptioncosts.BudgetListFilters, error) {
	query := r.URL.Query()
	filters := subscriptioncosts.BudgetListFilters{
		ScopeType: query.Get("scope_type"),
		ScopeID:   query.Get("scope_id"),
	}
	if raw := strings.TrimSpace(query.Get("enabled")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return subscriptioncosts.BudgetListFilters{}, err
		}
		filters.Enabled = &enabled
	}
	filters = subscriptioncosts.NormalizeBudgetListFilters(filters)
	if err := subscriptioncosts.ValidateBudgetListFilters(filters); err != nil {
		return subscriptioncosts.BudgetListFilters{}, err
	}
	return filters, nil
}
