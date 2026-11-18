package attachments

import (
	"encoding/json"
	"slices"
	"strings"
)

// LocalRecoveryDBSnapshot preserves complete stored rows, including historical
// incarnations and responsibilities whose business owners no longer exist.
// It is database evidence only: it does not attest to filesystem enumeration,
// stopped writers, physical identity reconciliation, or source admission.
// Raw rows intentionally retain fields omitted by live cleanup projections.
// Format 1 uses PostgreSQL JSON encodings for bytea, timestamps and integers;
// consumers must not decode numbers through float64.
type LocalRecoveryDBSnapshot struct {
	FormatVersion    uint32                       `json:"format_version"`
	SchemaRevision   string                       `json:"schema_revision"`
	PhysicalEvidence string                       `json:"physical_evidence"`
	Tables           []LocalRecoveryTableSnapshot `json:"tables"`
}

type LocalRecoveryTableSnapshot struct {
	Name            string            `json:"name"`
	Columns         []string          `json:"columns"`
	IdentityColumns []string          `json:"identity_columns"`
	Rows            []json.RawMessage `json:"rows"`
}

var localRecoveryTables = [...]string{
	"attachment_purge_preparations", "attachment_purge_receipts", "attachment_purge_targets",
	"blob_gc_deletions", "blob_gc_pins", "blob_publication_intents", "local_blob_gc_obligations",
	"local_blob_key_anchors", "local_blob_namespace_incarnations", "local_blob_namespaces",
	"record_portability_cleanup_preparations", "record_portability_cleanup_targets", "record_portability_purge_receipts",
	"record_write_obligation_targets", "record_write_obligations",
}

func (snapshot *LocalRecoveryDBSnapshot) Validate() error {
	if snapshot == nil || snapshot.FormatVersion != 1 || snapshot.SchemaRevision != "C81-local-io-v1" ||
		snapshot.PhysicalEvidence != "not_observed" || len(snapshot.Tables) != len(localRecoveryTables) {
		return ErrInvalidRecoveryRequest
	}
	for index, table := range snapshot.Tables {
		if table.Name != localRecoveryTables[index] || len(table.Columns) == 0 || len(table.IdentityColumns) == 0 || table.Rows == nil || !slices.IsSorted(table.Columns) {
			return ErrInvalidRecoveryRequest
		}
		for columnIndex, column := range table.Columns {
			if column == "" || (columnIndex > 0 && column == table.Columns[columnIndex-1]) {
				return ErrInvalidRecoveryRequest
			}
		}
		for _, column := range table.IdentityColumns {
			if !slices.Contains(table.Columns, column) {
				return ErrInvalidRecoveryRequest
			}
		}
		identities := make(map[string]struct{}, len(table.Rows))
		for _, row := range table.Rows {
			var fields map[string]json.RawMessage
			if json.Unmarshal(row, &fields) != nil || len(fields) != len(table.Columns) {
				return ErrInvalidRecoveryRequest
			}
			for _, column := range table.Columns {
				if _, ok := fields[column]; !ok {
					return ErrInvalidRecoveryRequest
				}
			}
			identity := make([]json.RawMessage, len(table.IdentityColumns))
			for keyIndex, column := range table.IdentityColumns {
				identity[keyIndex] = fields[column]
				if strings.TrimSpace(string(identity[keyIndex])) == "null" {
					return ErrInvalidRecoveryRequest
				}
			}
			encoded, err := json.Marshal(identity)
			if err != nil {
				return ErrInvalidRecoveryRequest
			}
			key := string(encoded)
			if _, exists := identities[key]; exists {
				return ErrInvalidRecoveryRequest
			}
			identities[key] = struct{}{}
		}
	}
	return nil
}

func (snapshot *LocalRecoveryDBSnapshot) clone() *LocalRecoveryDBSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.Tables = make([]LocalRecoveryTableSnapshot, len(snapshot.Tables))
	for index, table := range snapshot.Tables {
		copy.Tables[index] = LocalRecoveryTableSnapshot{Name: table.Name, Columns: slices.Clone(table.Columns), IdentityColumns: slices.Clone(table.IdentityColumns), Rows: make([]json.RawMessage, len(table.Rows))}
		for rowIndex, row := range table.Rows {
			copy.Tables[index].Rows[rowIndex] = slices.Clone(row)
		}
	}
	return &copy
}
