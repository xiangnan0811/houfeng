package syncqueue

import (
	"maps"
	"slices"

	"houfeng/internal/contracts/agentapi"
)

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Child slices have already been copied by cloneIPQualityReports.
func cloneIPQualityProductFields(report *agentapi.IPQualityReportPayload) {
	if report.Product != nil {
		product := *report.Product
		product.PTRNames = slices.Clone(product.PTRNames)
		product.Mail = slices.Clone(product.Mail)
		for i := range product.Mail {
			row := &product.Mail[i]
			row.MXPreference = clonePointer(row.MXPreference)
			row.GreetingCode = clonePointer(row.GreetingCode)
			row.LatencyMS = clonePointer(row.LatencyMS)
		}
		report.Product = &product
	}
	for i := range report.ProviderResults {
		row := &report.ProviderResults[i]
		row.ExtraJSON = slices.Clone(row.ExtraJSON)
		if row.Details == nil {
			continue
		}
		details := *row.Details
		details.Latitude = clonePointer(details.Latitude)
		details.Longitude = clonePointer(details.Longitude)
		details.IsMobile = clonePointer(details.IsMobile)
		details.IsAnycast = clonePointer(details.IsAnycast)
		details.IsBogon = clonePointer(details.IsBogon)
		details.FieldStates = maps.Clone(details.FieldStates)
		details.Scores = slices.Clone(details.Scores)
		for j := range details.Scores {
			score := &details.Scores[j]
			score.Value = clonePointer(score.Value)
			score.Min = clonePointer(score.Min)
			score.Max = clonePointer(score.Max)
		}
		row.Details = &details
	}
	for i := range report.ServiceUnlocks {
		row := &report.ServiceUnlocks[i]
		row.ExtraJSON = slices.Clone(row.ExtraJSON)
		row.Checks = slices.Clone(row.Checks)
		for j := range row.Checks {
			row.Checks[j].HTTPStatus = clonePointer(row.Checks[j].HTTPStatus)
		}
	}
}
