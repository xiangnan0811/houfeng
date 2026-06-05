package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresAssetDecisionRepositoryLoadsFactsWithAggregateQuery(t *testing.T) {
	now := time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC)
	capturedSQL := ""
	repo := &PostgresAssetDecisionRepository{db: fakeAssetDecisionQueryer{
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			capturedSQL = sql
			providerID := "pv_001"
			renewAt := now.AddDate(0, 0, 7)
			return &fakeAssetDecisionRows{rows: []fakeAssetDecisionScan{{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "vps_001"
				*(dest[1].(*string)) = "Frankfurt Primary"
				*(dest[2].(**string)) = &providerID
				*(dest[3].(*string)) = "Hetzner"
				*(dest[4].(*string)) = "CX22"
				*(dest[5].(*string)) = "order-1"
				*(dest[6].(*string)) = "Germany"
				*(dest[7].(*string)) = "Hesse"
				*(dest[8].(*string)) = "Falkenstein"
				*(dest[9].(*string)) = "FSN1"
				*(dest[10].(*string)) = "192.0.2.10"
				*(dest[11].(*string)) = ""
				*(dest[12].(*string)) = "192.0.2.10"
				*(dest[13].(*int)) = 22
				*(dest[14].(*string)) = "root"
				*(dest[15].(*string)) = "Debian"
				*(dest[16].(*string)) = "kvm"
				*(dest[17].(*vpsassets.LifecycleStatus)) = vpsassets.LifecycleActive
				*(dest[18].(*vpsassets.UsageStatus)) = vpsassets.UsageInUse
				*(dest[19].(*vpsassets.RenewalDecision)) = vpsassets.RenewalUnreviewed
				*(dest[20].(*string)) = "high"
				*(dest[21].(*[]string)) = []string{"edge"}
				*(dest[22].(*string)) = "note"
				*(dest[23].(*int)) = 2
				*(dest[24].(*int)) = 2
				*(dest[25].(*int)) = 1
				*(dest[26].(*time.Time)) = now
				*(dest[27].(*time.Time)) = now
				*(dest[28].(**time.Time)) = nil
				*(dest[29].(*int)) = 1
				*(dest[30].(*int)) = 1
				*(dest[31].(*int)) = 0
				*(dest[32].(*int)) = 2
				*(dest[33].(*int)) = 1
				*(dest[34].(*int)) = 1
				*(dest[35].(*int)) = 1
				*(dest[36].(*int)) = 2
				*(dest[37].(*int)) = 2
				*(dest[38].(*int)) = 1
				*(dest[39].(*int)) = 3
				*(dest[40].(*string)) = "CPU steal 高"
				*(dest[41].(*bool)) = true
				*(dest[42].(*string)) = "sub_001"
				*(dest[43].(*string)) = "vps_001"
				*(dest[44].(*float64)) = 12
				*(dest[45].(*string)) = "USD"
				*(dest[46].(*string)) = "monthly"
				*(dest[47].(*int)) = 1
				*(dest[48].(*string)) = "month"
				*(dest[49].(*int)) = 1
				*(dest[50].(*float64)) = 12
				*(dest[51].(**time.Time)) = nil
				*(dest[52].(**time.Time)) = &renewAt
				*(dest[53].(*bool)) = true
				*(dest[54].(*bool)) = false
				*(dest[55].(*string)) = "auto"
				*(dest[56].(*subscriptions.Status)) = subscriptions.StatusActive
				*(dest[57].(*string)) = "card"
				*(dest[58].(*string)) = "Frankfurt Primary"
				*(dest[59].(*string)) = "prod"
				*(dest[60].(*[]string)) = []string{"prod"}
				*(dest[61].(**time.Time)) = nil
				*(dest[62].(**time.Time)) = nil
				*(dest[63].(*string)) = "subscription note"
				*(dest[64].(**time.Time)) = &now
				*(dest[65].(**time.Time)) = &now
				return nil
			}}}}, nil
		},
	}}

	groups, err := repo.ListGroups(context.Background(), assetdecisions.ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if len(groups) == 0 {
		t.Fatal("groups = empty, want derived groups from loaded fact")
	}
	for _, want := range []string{
		"from vps_assets",
		"primary_subscriptions",
		"subscription_rollup",
		"from asset_services",
		"from asset_domains",
		"from vps_monitoring_instance_links",
		"join monitoring_instances",
		"join targets",
		"left join providers",
	} {
		if !strings.Contains(capturedSQL, want) {
			t.Fatalf("capturedSQL = %q, want %q", capturedSQL, want)
		}
	}
}

func TestPostgresAssetDecisionRepositoryGetGroupMissing(t *testing.T) {
	repo := &PostgresAssetDecisionRepository{db: fakeAssetDecisionQueryer{
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeAssetDecisionRows{}, nil
		},
	}}

	_, err := repo.GetGroup(context.Background(), "adg_auto_missing", assetdecisions.ListFilters{RenewWithinDays: 30})
	if err != assetdecisions.ErrAssetDecisionGroupNotFound {
		t.Fatalf("GetGroup() error = %v, want not found sentinel", err)
	}
}

type fakeAssetDecisionQueryer struct {
	query func(context.Context, string, ...any) (pgx.Rows, error)
}

func (f fakeAssetDecisionQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeAssetDecisionRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

type fakeAssetDecisionScan struct {
	scan func(dest ...any) error
}

type fakeAssetDecisionRows struct {
	rows []fakeAssetDecisionScan
	idx  int
	err  error
}

func (r *fakeAssetDecisionRows) Close() {}

func (r *fakeAssetDecisionRows) Err() error {
	return r.err
}

func (r *fakeAssetDecisionRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeAssetDecisionRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeAssetDecisionRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeAssetDecisionRows) Scan(dest ...any) error {
	return r.rows[r.idx-1].scan(dest...)
}

func (r *fakeAssetDecisionRows) Values() ([]any, error) {
	return nil, nil
}

func (r *fakeAssetDecisionRows) RawValues() [][]byte {
	return nil
}

func (r *fakeAssetDecisionRows) Conn() *pgx.Conn {
	return nil
}
