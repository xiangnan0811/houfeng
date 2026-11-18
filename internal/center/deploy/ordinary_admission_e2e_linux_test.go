//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// These checks run against the same owned Docker databases as the cold E2E.
func ordinaryColdAdmissionConnections(t *testing.T, f *ordinaryColdFixture) (*pgx.Conn, *pgx.Conn) {
	t.Helper()
	body, err := os.ReadFile(f.request)
	var request ordinaryUpgradeRequest
	if err != nil || json.Unmarshal(body, &request) != nil {
		t.Fatal("invalid fixture request")
	}
	password, err := os.ReadFile(request.Secrets.Bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimSpace(string(coldCommand(t, f.ctx, "/usr/bin/docker", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", request.DatabaseContainerID)))
	if net.ParseIP(address) == nil {
		t.Fatal("expected one database network address")
	}
	observer, _, err := ordinaryOpenControlObserver(f.ctx, address, string(password), f.operation)
	if err != nil {
		t.Fatal("open cold observer")
	}
	endpoint := &url.URL{Scheme: "postgres", User: url.UserPassword(composeBootstrapRole, string(password)), Host: net.JoinHostPort(address, strconv.Itoa(int(composeDatabasePort))), Path: "/" + composeDatabaseName, RawQuery: "sslmode=disable"}
	writer, err := pgx.Connect(f.ctx, endpoint.String())
	if err != nil {
		ordinaryCloseControlObserver(observer)
		t.Fatal("open fixture writer")
	}
	return observer, writer
}

func ordinaryColdAssertC80Admission(t *testing.T, f *ordinaryColdFixture) {
	t.Helper()
	observer, writer := ordinaryColdAdmissionConnections(t, f)
	defer ordinaryCloseControlObserver(observer)
	defer writer.Close(context.Background())
	if class, err := ordinaryObservePredecessorClass(f.ctx, observer); err != nil || class != ordinaryPredecessorC80 {
		t.Fatalf("exact released C80 rejected: %q %v", class, err)
	}
	if _, err := writer.Exec(f.ctx, `create table public.source_application_principals (principal text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryObservePredecessorClass(f.ctx, observer); err == nil {
		t.Fatal("accepted partial C81 source object")
	}
	if _, err := writer.Exec(f.ctx, `drop table public.source_application_principals`); err != nil {
		t.Fatal(err)
	}
	var checksum string
	if err := writer.QueryRow(f.ctx, `select checksum from public.schema_migrations where name='0080_bound_agent_sync_replay.sql'`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(f.ctx, `update public.schema_migrations set checksum=repeat('0',64) where name='0080_bound_agent_sync_replay.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryObservePredecessorClass(f.ctx, observer); err == nil {
		t.Fatal("accepted tampered C80 migration checksum")
	}
	if _, err := writer.Exec(f.ctx, `update public.schema_migrations set checksum=$1 where name='0080_bound_agent_sync_replay.sql'`, checksum); err != nil {
		t.Fatal(err)
	}
	if class, err := ordinaryObservePredecessorClass(f.ctx, observer); err != nil || class != ordinaryPredecessorC80 {
		t.Fatalf("restored C80 rejected: %q %v", class, err)
	}
}

func ordinaryColdAssertC81SourceRefused(t *testing.T, f *ordinaryColdFixture) {
	t.Helper()
	observer, writer := ordinaryColdAdmissionConnections(t, f)
	defer ordinaryCloseControlObserver(observer)
	defer writer.Close(context.Background())
	if class, err := ordinaryObservePredecessorClass(f.ctx, observer); err != nil || class != ordinaryPredecessorC81 {
		t.Fatalf("ordinary C81 rejected: %q %v", class, err)
	}
	var checksum string
	if err := writer.QueryRow(f.ctx, `select checksum from public.schema_migrations where name='0081_record_deletion_successor.sql'`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(f.ctx, `update public.schema_migrations set checksum=repeat('0',64) where name='0081_record_deletion_successor.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryObservePredecessorClass(f.ctx, observer); err == nil {
		t.Fatal("accepted wrong-checksum C81")
	}
	if _, err := writer.Exec(f.ctx, `update public.schema_migrations set checksum=$1 where name='0081_record_deletion_successor.sql'`, checksum); err != nil {
		t.Fatal(err)
	}
	if class, err := ordinaryObservePredecessorClass(f.ctx, observer); err != nil || class != ordinaryPredecessorC81 {
		t.Fatal("restored C81 rejected")
	}
	_, err := writer.Exec(f.ctx, `insert into public.source_authority_admission (deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode) values ('dp-'||repeat('a',64),'rd-'||repeat('b',64),'1',1,'fixture',1,1,'closed')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryObservePredecessorClass(f.ctx, observer); err == nil {
		t.Fatal("accepted source-installed C81")
	}
	if _, err := writer.Exec(f.ctx, `delete from public.source_authority_admission`); err != nil {
		t.Fatal(err)
	}
}
