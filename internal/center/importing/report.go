package importing

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func WriteReport(w io.Writer, report Report, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return WriteTextReport(w, report)
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	default:
		return fmt.Errorf("unsupported report format %q", format)
	}
}

func WriteTextReport(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "VPS JSON import report\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "mode: %s\ncurrent_date: %s\ndatabase_checked: %t\ncan_import: %t\n\n", report.Mode, report.CurrentDate, report.DatabaseChecked, report.CanImport); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "totals:\n"); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("input_rows: %d", report.Totals.InputRows),
		fmt.Sprintf("provider_create_candidates: %d", report.Totals.ProviderCreateCandidates),
		fmt.Sprintf("vps_create_candidates: %d", report.Totals.VPSCreateCandidates),
		fmt.Sprintf("subscription_candidates: %d", report.Totals.SubscriptionCandidates),
		fmt.Sprintf("missing_provider_rows: %d", report.Totals.MissingProviderRows),
		fmt.Sprintf("missing_renew_date_rows: %d", report.Totals.MissingRenewDateRows),
		fmt.Sprintf("validation_errors: %d", report.Totals.ValidationErrors),
		fmt.Sprintf("duplicate_candidates: %d", report.Totals.DuplicateCandidates),
		fmt.Sprintf("node_association_candidates: %d", report.Totals.NodeAssociationCandidates),
		fmt.Sprintf("renewal_candidates: %d", report.Totals.RenewalCandidates),
		fmt.Sprintf("idle_paid_candidates: %d", report.Totals.IdlePaidCandidates),
		fmt.Sprintf("imported_providers: %d", report.Totals.ImportedProviders),
		fmt.Sprintf("imported_vps_assets: %d", report.Totals.ImportedVPSAssets),
		fmt.Sprintf("imported_subscriptions: %d", report.Totals.ImportedSubscriptions),
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
			return err
		}
	}
	if err := writeWarnings(w, report.Warnings); err != nil {
		return err
	}
	if err := writeProviderCandidates(w, report.ProviderCandidates); err != nil {
		return err
	}
	if err := writeRowIssues(w, "missing provider rows", report.MissingProviderRows); err != nil {
		return err
	}
	if err := writeRowIssues(w, "missing renew date rows", report.MissingRenewDateRows); err != nil {
		return err
	}
	if err := writeRowIssues(w, "validation errors", report.ValidationErrors); err != nil {
		return err
	}
	if err := writeDuplicateCandidates(w, report.DuplicateCandidates); err != nil {
		return err
	}
	if err := writeNodeCandidates(w, report.NodeAssociationCandidates); err != nil {
		return err
	}
	if err := writeRenewalCandidates(w, report.RenewalCandidates); err != nil {
		return err
	}
	if err := writeIdlePaidCandidates(w, report.IdlePaidCandidates); err != nil {
		return err
	}
	return writeImportResult(w, report.Import)
}

func writeWarnings(w io.Writer, warnings []string) error {
	if _, err := fmt.Fprintf(w, "\nwarnings:\n"); err != nil {
		return err
	}
	if len(warnings) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(w, "  %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func writeProviderCandidates(w io.Writer, candidates []ProviderCandidate) error {
	if _, err := fmt.Fprintf(w, "\nprovider candidates:\n"); err != nil {
		return err
	}
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, candidate := range candidates {
		if _, err := fmt.Fprintf(w, "  %s rows=%v\n", candidate.Name, candidate.Rows); err != nil {
			return err
		}
	}
	return nil
}

func writeRowIssues(w io.Writer, title string, issues []RowIssue) error {
	if _, err := fmt.Fprintf(w, "\n%s:\n", title); err != nil {
		return err
	}
	if len(issues) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, issue := range issues {
		if _, err := fmt.Fprintf(w, "  row %d %s: %s\n", issue.Row, issue.Field, issue.Message); err != nil {
			return err
		}
	}
	return nil
}

func writeDuplicateCandidates(w io.Writer, candidates []DuplicateCandidate) error {
	if _, err := fmt.Fprintf(w, "\nduplicate candidates:\n"); err != nil {
		return err
	}
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, candidate := range candidates {
		if _, err := fmt.Fprintf(w, "  %s %s rows=%v existing_id=%s: %s\n", candidate.Type, candidate.Key, candidate.Rows, candidate.ExistingID, candidate.Message); err != nil {
			return err
		}
	}
	return nil
}

func writeNodeCandidates(w io.Writer, candidates []NodeAssociationCandidate) error {
	if _, err := fmt.Fprintf(w, "\nnode association candidates:\n"); err != nil {
		return err
	}
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, candidate := range candidates {
		if _, err := fmt.Fprintf(w, "  row %d %s node_id=%s node_name=%s target_url=%s: %s\n", candidate.Row, candidate.DisplayName, candidate.NodeID, candidate.NodeName, candidate.TargetURL, candidate.Status); err != nil {
			return err
		}
	}
	return nil
}

func writeRenewalCandidates(w io.Writer, candidates []RenewalCandidate) error {
	if _, err := fmt.Fprintf(w, "\nrenewal candidates:\n"); err != nil {
		return err
	}
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, candidate := range candidates {
		if _, err := fmt.Fprintf(w, "  row %d %s renew_at=%s days=%d %.2f %s monthly=%.4f\n", candidate.Row, candidate.DisplayName, candidate.RenewAt, candidate.DaysUntil, candidate.Price, candidate.Currency, candidate.MonthlyPrice); err != nil {
			return err
		}
	}
	return nil
}

func writeIdlePaidCandidates(w io.Writer, candidates []IdlePaidCandidate) error {
	if _, err := fmt.Fprintf(w, "\nidle paid candidates:\n"); err != nil {
		return err
	}
	if len(candidates) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, candidate := range candidates {
		renewAt := ""
		if candidate.RenewAt != nil {
			renewAt = *candidate.RenewAt
		}
		if _, err := fmt.Fprintf(w, "  row %d %s renew_at=%s %.2f %s monthly=%.4f\n", candidate.Row, candidate.DisplayName, renewAt, candidate.Price, candidate.Currency, candidate.MonthlyPrice); err != nil {
			return err
		}
	}
	return nil
}

func writeImportResult(w io.Writer, result ImportResult) error {
	if _, err := fmt.Fprintf(w, "\nimport result:\n"); err != nil {
		return err
	}
	if len(result.CreatedProviders) == 0 && len(result.CreatedVPSAssets) == 0 && len(result.CreatedSubscriptions) == 0 {
		_, err := fmt.Fprintln(w, "  (none)")
		return err
	}
	for _, provider := range result.CreatedProviders {
		if _, err := fmt.Fprintf(w, "  provider %s %s\n", provider.ProviderID, provider.Name); err != nil {
			return err
		}
	}
	for _, asset := range result.CreatedVPSAssets {
		if _, err := fmt.Fprintf(w, "  vps row=%d %s %s\n", asset.Row, asset.VPSID, asset.DisplayName); err != nil {
			return err
		}
	}
	for _, subscription := range result.CreatedSubscriptions {
		if _, err := fmt.Fprintf(w, "  subscription row=%d %s vps=%s\n", subscription.Row, subscription.SubscriptionID, subscription.VPSID); err != nil {
			return err
		}
	}
	return nil
}
