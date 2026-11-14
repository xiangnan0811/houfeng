package recordbirth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyRuntimeLaunch verifies the published launch against APP's current
// birth binding and the independent W2 continuity reader. No APP transaction
// remains open while W2 is contacted. The caller retains ownership of pools.
func VerifyRuntimeLaunch(ctx context.Context, app *pgxpool.Pool, reader *PostgresAuthority, launch LaunchBinding) error {
	if ctx == nil || app == nil || validateLaunchBinding(launch) != nil {
		return fmt.Errorf("%w: invalid runtime launch verification", ErrInvalid)
	}
	if launch.BirthInstalled {
		expected := postgresPhysicalIdentity{
			ID: launch.Witness.ID, SystemIdentifier: launch.Witness.SystemIdentifier,
			DatabaseOID: launch.Witness.DatabaseOID, DatabaseName: launch.Witness.DatabaseName,
			Epoch: launch.Witness.Epoch,
		}
		if reader == nil || reader.pool == nil || reader.pool == app || reader.application != app ||
			reader.expected == nil || *reader.expected != expected {
			return fmt.Errorf("%w: launch witness reader mismatch", ErrUnavailable)
		}
	} else if reader != nil {
		return fmt.Errorf("%w: unexpected launch witness reader", ErrUnavailable)
	}

	bindingBytes, err := readRuntimeLaunchBinding(ctx, app)
	if err != nil {
		return err
	}
	proof := []byte{}
	if launch.BirthInstalled {
		if bindingBytes == nil || len(bindingBytes) == 0 || len(bindingBytes) > maxLaunchBindingBytes {
			return fmt.Errorf("%w: installed APP birth binding unavailable", ErrUnavailable)
		}
		var actual postgresBirthBinding
		decoder := json.NewDecoder(bytes.NewReader(bindingBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&actual); err != nil {
			return fmt.Errorf("%w: invalid APP birth binding", ErrUnavailable)
		}
		if err := decoder.Decode(new(any)); err != io.EOF || actual != postgresBinding(launch.BirthBinding) {
			return fmt.Errorf("%w: APP launch birth binding mismatch", ErrUnavailable)
		}
		observation, err := reader.Continuity(ctx, launch.BirthBinding)
		if err != nil {
			return fmt.Errorf("%w: launch continuity unavailable: %w", ErrUnavailable, err)
		}
		if observation.Binding != launch.BirthBinding || len(observation.Proof) == 0 {
			return fmt.Errorf("%w: invalid launch continuity observation", ErrUnavailable)
		}
		proof = observation.Proof
	} else if bindingBytes != nil {
		// Only SQL NULL proves there was no APP binding in this observation.
		return fmt.Errorf("%w: unexpected APP birth binding", ErrUnavailable)
	}

	// The assertion authenticates the proof, current source/binding and APP
	// canonical ancestry. An empty bytea, not SQL NULL, denotes no installation.
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return postgresBirthError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, proof); err != nil {
		return postgresBirthError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return postgresBirthError(err)
	}
	return nil
}

func readRuntimeLaunchBinding(ctx context.Context, app *pgxpool.Pool) ([]byte, error) {
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, postgresBirthError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	if err := tx.QueryRow(ctx, `select public.record_birth_continuity_binding()`).Scan(&raw); err != nil {
		return nil, postgresBirthError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, postgresBirthError(err)
	}
	return raw, nil
}
