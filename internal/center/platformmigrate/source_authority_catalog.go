package platformmigrate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// checkWitnessCatalog compares the live predecessor/successor to a temporary
// catalog instantiated from the frozen migration text. The savepoint removes
// every temporary object before returning; live rows, ownership and ACLs are
// never used as the reference oracle. Ownership and effective ACLs are checked
// separately against the declared principals.
func checkWitnessCatalog(ctx context.Context, tx pgx.Tx, source, prefix string, tables, functions []string) error {
	if _, err := tx.Exec(ctx, `savepoint witness_catalog_reference`); err != nil {
		return fmt.Errorf("begin witness catalog reference: %w", err)
	}
	prefixes := []string{prefix}
	if prefix == "witness_foundation" {
		start := strings.Index(source, "create table public.record_platform_domain_identity")
		if start < 0 {
			return fmt.Errorf("witness foundation migration source is incomplete")
		}
		source = source[start:]
		prefixes = []string{"record_platform_domain_", "deletion_witness_", "recovery_trust_witness_"}
	}
	reference := source
	for _, name := range prefixes {
		reference = strings.ReplaceAll(reference, "public."+name, "pg_temp."+name)
		reference = strings.ReplaceAll(reference, "record_platform_internal."+name, "pg_temp."+name)
	}
	if _, err := tx.Exec(ctx, reference); err != nil {
		return fmt.Errorf("instantiate witness catalog reference: %w", err)
	}
	var temporarySchema string
	if err := tx.QueryRow(ctx, `select nspname from pg_namespace where oid=pg_my_temp_schema()`).Scan(&temporarySchema); err != nil {
		return fmt.Errorf("read witness catalog reference schema: %w", err)
	}
	normalize := func(value string) string {
		for _, name := range prefixes {
			for _, qualifier := range []string{"public.", "record_platform_internal.", "pg_temp.", temporarySchema + "."} {
				value = strings.ReplaceAll(value, qualifier+name, name)
			}
		}
		return value
	}
	for _, table := range tables {
		actual, expected := "public."+table, "pg_temp."+table
		for _, query := range []string{
			`select coalesce(array_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||
				a.attnotnull::text||':'||a.attisdropped::text||':'||a.attidentity::text||':'||a.attgenerated::text||':'||
				coalesce(pg_get_expr(d.adbin,d.adrelid),'') order by a.attnum),array[]::text[])
			 from pg_attribute a left join pg_attrdef d on d.adrelid=a.attrelid and d.adnum=a.attnum
			 where a.attrelid=to_regclass($1) and a.attnum>0`,
			`select coalesce(array_agg(c.conname||':'||c.contype::text||':'||c.convalidated::text||':'||
				c.condeferrable::text||':'||c.condeferred::text||':'||pg_get_constraintdef(c.oid,true)
				order by c.conname),array[]::text[])
			 from pg_constraint c where c.conrelid=to_regclass($1)`,
			`select coalesce(array_agg(t.tgname||':'||t.tgtype::text||':'||t.tgenabled::text||':'||
				t.tgfoid::regprocedure::text order by t.tgname),array[]::text[])
			 from pg_trigger t where t.tgrelid=to_regclass($1) and not t.tgisinternal`,
		} {
			var got, want []string
			if err := tx.QueryRow(ctx, query, actual).Scan(&got); err != nil {
				return fmt.Errorf("inspect witness catalog %s: %w", table, err)
			}
			if err := tx.QueryRow(ctx, query, expected).Scan(&want); err != nil {
				return fmt.Errorf("inspect witness catalog reference %s: %w", table, err)
			}
			for index := range got {
				got[index] = normalize(got[index])
			}
			for index := range want {
				want[index] = normalize(want[index])
			}
			if !slices.Equal(got, want) {
				return fmt.Errorf("witness catalog definition mismatch for %s", table)
			}
		}
	}
	for _, function := range functions {
		referenceFunction := function
		for _, name := range prefixes {
			referenceFunction = strings.ReplaceAll(referenceFunction, "public."+name, "pg_temp."+name)
			referenceFunction = strings.ReplaceAll(referenceFunction, "record_platform_internal."+name, "pg_temp."+name)
		}
		const functionQuery = `select array[prosrc,prosecdef::text,provolatile::text,proparallel::text,
			proisstrict::text,proretset::text,proleakproof::text,coalesce(proconfig::text,''),
			pg_get_function_result(oid)] from pg_proc where oid=to_regprocedure($1)`
		var got, want []string
		if err := tx.QueryRow(ctx, functionQuery, function).Scan(&got); err != nil {
			return fmt.Errorf("inspect witness function %s: %w", function, err)
		}
		if err := tx.QueryRow(ctx, functionQuery, referenceFunction).Scan(&want); err != nil {
			return fmt.Errorf("inspect witness reference function %s: %w", function, err)
		}
		for index := range got {
			got[index] = normalize(got[index])
		}
		for index := range want {
			want[index] = normalize(want[index])
		}
		if !slices.Equal(got, want) {
			return fmt.Errorf("witness function definition mismatch for %s", function)
		}
	}
	if prefix == "source_" {
		var got, want []string
		const sequenceQuery = `select array[seqstart::text,seqincrement::text,seqmax::text,
			seqmin::text,seqcache::text,seqcycle::text] from pg_sequence where seqrelid=to_regclass($1)`
		if err := tx.QueryRow(ctx, sequenceQuery, sourceAuthoritySequence).Scan(&got); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, sequenceQuery, "pg_temp.source_retirement_prepares_prepare_sequence_seq").Scan(&want); err != nil {
			return err
		}
		if !slices.Equal(got, want) {
			return fmt.Errorf("witness identity sequence definition mismatch")
		}
	}
	if _, err := tx.Exec(ctx, `rollback to savepoint witness_catalog_reference`); err != nil {
		return fmt.Errorf("discard witness catalog reference: %w", err)
	}
	if _, err := tx.Exec(ctx, `release savepoint witness_catalog_reference`); err != nil {
		return fmt.Errorf("release witness catalog reference: %w", err)
	}
	return nil
}

// The frozen W1 trigger function protects both original and successor history;
// matching trigger names alone cannot detect a replaced permissive body.
func checkWitnessImmutableFunction(ctx context.Context, tx pgx.Tx, source string) error {
	start := strings.Index(source, "create or replace function record_platform_internal.reject_immutable_mutation()")
	if start < 0 {
		return fmt.Errorf("witness immutable function source is absent")
	}
	definition := source[start:]
	start = strings.Index(definition, "as $$")
	if start < 0 {
		return fmt.Errorf("witness immutable function body is absent")
	}
	definition = definition[start+len("as $$"):]
	end := strings.Index(definition, "$$;")
	if end < 0 {
		return fmt.Errorf("witness immutable function body is incomplete")
	}
	var body, configuration string
	var secure, strict bool
	if err := tx.QueryRow(ctx, `select prosrc,coalesce(proconfig::text,''),prosecdef,proisstrict
		from pg_proc where oid=to_regprocedure('record_platform_internal.reject_immutable_mutation()')`).Scan(
		&body, &configuration, &secure, &strict); err != nil {
		return fmt.Errorf("inspect witness immutable function: %w", err)
	}
	if body != definition[:end] || configuration != "{search_path=pg_catalog}" || !secure || strict {
		return fmt.Errorf("witness immutable function differs from frozen migration")
	}
	return nil
}
