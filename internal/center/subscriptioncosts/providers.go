package subscriptioncosts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"houfeng/internal/center/subscriptions"
)

type HTTPExchangeRateProvider struct {
	baseURL      string
	client       *http.Client
	apiKey       string
	kind         string
	settingsRepo SettingsRepository
}

func NewFrankfurterProvider(client *http.Client) ExchangeRateProvider {
	return &HTTPExchangeRateProvider{
		baseURL: "https://api.frankfurter.dev/v2",
		client:  clientOrDefault(client),
		kind:    "frankfurter",
	}
}

func NewFixerProvider(client *http.Client, apiKey string) ExchangeRateProvider {
	return &HTTPExchangeRateProvider{
		baseURL: "https://data.fixer.io/api",
		client:  clientOrDefault(client),
		apiKey:  strings.TrimSpace(apiKey),
		kind:    "fixer",
	}
}

func NewSettingsAwareFixerProvider(client *http.Client, settingsRepo SettingsRepository) ExchangeRateProvider {
	return &HTTPExchangeRateProvider{
		baseURL:      "https://data.fixer.io/api",
		client:       clientOrDefault(client),
		kind:         "fixer",
		settingsRepo: settingsRepo,
	}
}

func (p *HTTPExchangeRateProvider) FetchRate(ctx context.Context, quoteCurrency, baseCurrency string) (FetchedExchangeRate, error) {
	quoteCurrency = strings.ToUpper(strings.TrimSpace(quoteCurrency))
	baseCurrency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	if quoteCurrency == "" || baseCurrency == "" || quoteCurrency == baseCurrency {
		return FetchedExchangeRate{}, fmt.Errorf("%w: invalid currency pair", ErrInvalidInput)
	}
	switch p.kind {
	case "frankfurter":
		return p.fetchFrankfurter(ctx, quoteCurrency, baseCurrency)
	case "fixer":
		return p.fetchFixer(ctx, quoteCurrency, baseCurrency)
	default:
		return FetchedExchangeRate{}, fmt.Errorf("%w: unknown exchange rate provider", ErrInvalidInput)
	}
}

func (p *HTTPExchangeRateProvider) fetchFrankfurter(ctx context.Context, quoteCurrency, baseCurrency string) (FetchedExchangeRate, error) {
	endpoint := strings.TrimRight(p.baseURL, "/") + "/rate/" + url.PathEscape(quoteCurrency) + "/" + url.PathEscape(baseCurrency)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return FetchedExchangeRate{}, fmt.Errorf("build frankfurter request: %w", err)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return FetchedExchangeRate{}, fmt.Errorf("fetch frankfurter rate: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return FetchedExchangeRate{}, fmt.Errorf("frankfurter returned status %d", res.StatusCode)
	}
	var payload struct {
		Rate float64 `json:"rate"`
		Date string  `json:"date"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return FetchedExchangeRate{}, fmt.Errorf("decode frankfurter response: %w", err)
	}
	return normalizeFetchedRate(payload.Rate, payload.Date)
}

func (p *HTTPExchangeRateProvider) fetchFixer(ctx context.Context, quoteCurrency, baseCurrency string) (FetchedExchangeRate, error) {
	apiKey := strings.TrimSpace(p.apiKey)
	if p.settingsRepo != nil {
		settings, err := p.settingsRepo.GetSettings(ctx)
		if err != nil {
			return FetchedExchangeRate{}, fmt.Errorf("get fixer settings: %w", err)
		}
		apiKey = strings.TrimSpace(settings.SubscriptionCost.FixerAPIKey)
	}
	if apiKey == "" {
		return FetchedExchangeRate{}, fmt.Errorf("%w: fixer api key is not configured", ErrInvalidInput)
	}
	values := url.Values{}
	values.Set("access_key", apiKey)
	values.Set("base", quoteCurrency)
	values.Set("symbols", baseCurrency)
	endpoint := strings.TrimRight(p.baseURL, "/") + "/latest?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return FetchedExchangeRate{}, fmt.Errorf("build fixer request: %w", err)
	}
	res, err := p.client.Do(req)
	if err != nil {
		return FetchedExchangeRate{}, fmt.Errorf("fetch fixer rate: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return FetchedExchangeRate{}, fmt.Errorf("fixer returned status %d", res.StatusCode)
	}
	var payload struct {
		Success bool               `json:"success"`
		Date    string             `json:"date"`
		Rates   map[string]float64 `json:"rates"`
		Error   fixerErrorResponse `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return FetchedExchangeRate{}, fmt.Errorf("decode fixer response: %w", err)
	}
	if !payload.Success {
		if payload.Error.Info != "" {
			return FetchedExchangeRate{}, errors.New(payload.Error.Info)
		}
		return FetchedExchangeRate{}, errors.New("fixer request failed")
	}
	rate, ok := payload.Rates[baseCurrency]
	if !ok {
		return FetchedExchangeRate{}, fmt.Errorf("fixer response missing %s rate", baseCurrency)
	}
	return normalizeFetchedRate(rate, payload.Date)
}

type fixerErrorResponse struct {
	Code int    `json:"code"`
	Type string `json:"type"`
	Info string `json:"info"`
}

func normalizeFetchedRate(rate float64, rawDate string) (FetchedExchangeRate, error) {
	if rate <= 0 {
		return FetchedExchangeRate{}, errors.New("exchange rate must be positive")
	}
	rateDate, err := subscriptions.ParseDate(strings.TrimSpace(rawDate))
	if err != nil {
		rateDate = subscriptions.NewDate(time.Now().UTC())
	}
	return FetchedExchangeRate{Rate: rate, RateDate: rateDate}, nil
}

func clientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}
