package store

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

// prepareSyncFixture migrates pre-v2 domain fixtures to a wire request first.
// The digest always comes from that exact DTO, never an invented hash. New
// protocol-admission tests construct explicit DTOs and bypass this adapter.
func prepareSyncFixture(batch syncing.Batch) syncing.Batch {
	if batch.SessionID == "" {
		batch.SessionID = "mas_fixture"
	}
	if batch.ProtocolRevision == 0 {
		batch.ProtocolRevision = agentapi.SyncProtocolRevision
	}
	if batch.ProducerID == "" {
		batch.ProducerID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(batch.SessionID)).String()
	}
	if batch.BatchSequence == 0 && len(batch.Heartbeats) > 0 {
		id := batch.Heartbeats[0].SyncBatchID
		if sequence, err := strconv.ParseInt(trimFixtureProducer(id, batch.ProducerID), 10, 64); err == nil && sequence > 0 {
			batch.BatchSequence = sequence
		} else {
			identity := sha256.Sum256([]byte(id))
			batch.BatchSequence = int64(binary.BigEndian.Uint64(identity[:8])%65536) + 1
		}
	}
	if batch.AllocatedThrough < batch.BatchSequence {
		batch.AllocatedThrough = batch.BatchSequence
	}
	identity := batch.ProducerID + ":" + strconv.FormatInt(batch.BatchSequence, 10)
	// Copy all slices: fixture variants often share their original backing array.
	batch.Heartbeats = append(batch.Heartbeats[:0:0], batch.Heartbeats...)
	batch.Observations.HostSamples = append(batch.Observations.HostSamples[:0:0], batch.Observations.HostSamples...)
	batch.Observations.ProbeObservations = append(batch.Observations.ProbeObservations[:0:0], batch.Observations.ProbeObservations...)
	batch.IPQualityReports = append(batch.IPQualityReports[:0:0], batch.IPQualityReports...)
	for i := range batch.Heartbeats {
		batch.Heartbeats[i].SyncBatchID = identity
	}
	for i := range batch.Observations.HostSamples {
		batch.Observations.HostSamples[i].SyncBatchID = identity
	}
	for i := range batch.Observations.ProbeObservations {
		batch.Observations.ProbeObservations[i].SyncBatchID = identity
	}
	for i := range batch.IPQualityReports {
		batch.IPQualityReports[i].SyncBatchID = identity
	}
	req := syncFixtureDTO(batch)
	var err error
	batch.PayloadDigest, err = agentapi.CanonicalSyncBatchDigest(req)
	if err != nil {
		panic(err)
	}
	return batch
}

func trimFixtureProducer(id, producer string) string {
	prefix := producer + ":"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return ""
}

func syncFixtureDTO(batch syncing.Batch) agentapi.SyncRequest {
	var req agentapi.SyncRequest
	copySyncFixtureFields(reflect.ValueOf(&req).Elem(), reflect.ValueOf(batch))
	copySyncFixtureFields(reflect.ValueOf(&req.HostSamples).Elem(), reflect.ValueOf(batch.Observations.HostSamples))
	for i, report := range batch.IPQualityReports {
		if len(report.CoverageJSON) > 0 && string(report.CoverageJSON) != "null" {
			if err := json.Unmarshal(report.CoverageJSON, &req.IPQualityReports[i].Coverage); err != nil {
				panic(err)
			}
		}
	}
	copySyncFixtureFields(reflect.ValueOf(&req.ProbeObservations).Elem(), reflect.ValueOf(batch.Observations.ProbeObservations))
	return req
}

func fixtureCanonicalBatchID(monitoringInstanceID, oldID string) string {
	batch := prepareSyncFixture(syncing.Batch{
		SessionID:  "mas_" + strings.TrimPrefix(monitoringInstanceID, "mi_"),
		Heartbeats: []syncing.HeartbeatPayload{{SyncBatchID: oldID}},
	})
	return batch.Heartbeats[0].SyncBatchID
}

func syncFixtureBatch(req agentapi.SyncRequest) syncing.Batch {
	var batch syncing.Batch
	copySyncFixtureFields(reflect.ValueOf(&batch).Elem(), reflect.ValueOf(req))
	batch.Observations.MonitoringInstanceID = req.MonitoringInstanceID
	copySyncFixtureFields(reflect.ValueOf(&batch.Observations.HostSamples).Elem(), reflect.ValueOf(req.HostSamples))
	copySyncFixtureFields(reflect.ValueOf(&batch.Observations.ProbeObservations).Elem(), reflect.ValueOf(req.ProbeObservations))
	for i := range batch.Observations.HostSamples {
		batch.Observations.HostSamples[i].MonitoringInstanceID = req.MonitoringInstanceID
	}
	for i := range batch.Observations.ProbeObservations {
		batch.Observations.ProbeObservations[i].MonitoringInstanceID = req.MonitoringInstanceID
	}
	for i := range batch.IPQualityReports {
		batch.IPQualityReports[i].MonitoringInstanceID = req.MonitoringInstanceID
	}
	var err error
	for i, report := range req.IPQualityReports {
		if report.Coverage != nil {
			raw, err := json.Marshal(report.Coverage)
			if err != nil {
				panic(err)
			}
			batch.IPQualityReports[i].CoverageJSON = raw
		}
	}
	batch.PayloadDigest, err = agentapi.CanonicalSyncBatchDigest(req)
	if err != nil {
		panic(err)
	}
	return batch
}

// Domain writes and wire payloads deliberately have separate types. Match
// exported field names recursively instead of relying on incompatible JSON tags;
// receiver-only fields have no wire destination and cannot enter the digest.
func copySyncFixtureFields(dst, src reflect.Value) {
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return
	}
	switch dst.Kind() {
	case reflect.Struct:
		for i := range dst.NumField() {
			field := src.FieldByName(dst.Type().Field(i).Name)
			if field.IsValid() && dst.Field(i).CanSet() {
				copySyncFixtureFields(dst.Field(i), field)
			}
		}
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
		for i := range src.Len() {
			copySyncFixtureFields(dst.Index(i), src.Index(i))
		}
	}
}
