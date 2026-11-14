//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recorddeletion"
)

type ledgerSourcePending struct {
	preview ledgerSourcePreview
	intent  ledgerSourceIntent
	entry   deletionledger.SourceRetirementEntry
	found   bool
}

func ledgerSourceEntryMatches(preview ledgerSourcePreview, row recorddeletion.LedgerChainEntry) (deletionledger.SourceRetirementEntry, bool) {
	if row.Route != "source_permanent_delete" {
		return deletionledger.SourceRetirementEntry{}, false
	}
	entry, err := deletionledger.DecodeSourceRetirementEntry(row.Canonical)
	if err != nil || !ledgerSourceRequestEqual(entry.Request, preview.Request) || entry.Sequence != preview.Anchor.Primary.Sequence+1 || entry.PreviousHash != preview.Anchor.Primary.Hash || row.Sequence != entry.Sequence || row.PreviousHash != entry.PreviousHash || row.EntryHash != entry.EntryHash {
		return deletionledger.SourceRetirementEntry{}, false
	}
	domain, err := recorddeletion.LedgerDomainDigest(preview.PrimaryIdentity.ID)
	if err != nil || entry.LedgerDomain != domain {
		return deletionledger.SourceRetirementEntry{}, false
	}
	return entry, true
}

// sourceJournalPair binds every recognized source row, including already
// anchored rows, to its immutable original request and APP pending provenance.
// The caller holds work while using the in-memory anchor inventory.
func (f *RecordLedgerFence) sourceJournalPair(primary, witness recorddeletion.LedgerSnapshot, anchor ledgerAnchor) ([]ledgerSourcePending, error) {
	previews, err := ledgerReadSourcePreviews(f.control)
	if err != nil {
		return nil, err
	}
	names, err := ledgerNames(f.control)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(previews))
	var pending []ledgerSourcePending
	for _, preview := range previews {
		known[preview.Key] = true
		if preview.Request.Origin.Application != ledgerSourceApplication(f.request.AppIdentity) || preview.PrimaryIdentity != primary.Identity || preview.WitnessIdentity != witness.Identity || preview.Anchor.Index >= uint64(len(f.anchors)) || f.anchors[preview.Anchor.Index] != preview.Anchor {
			return nil, ErrRecordLedgerFence
		}
		intent, exists, err := ledgerReadSourceIntent(f.control, preview)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		item := ledgerSourcePending{preview: preview, intent: intent}
		for _, row := range primary.Entries {
			entry, matches := ledgerSourceEntryMatches(preview, row)
			if !matches {
				continue
			}
			if item.found {
				return nil, ErrRecordLedgerFence
			}
			item.entry, item.found = entry, true
		}
		if !item.found && (primary.Head != preview.Anchor.Primary || witness.Head != preview.Anchor.Witness || anchor != preview.Anchor) {
			return nil, ErrRecordLedgerFence
		}
		pending = append(pending, item)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "source-intent-") {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(name, "source-intent-"), ".json")
		if !known[key] || name != "source-intent-"+key+".json" {
			return nil, ErrRecordLedgerFence
		}
	}
	for _, row := range primary.Entries {
		if row.Route != "source_permanent_delete" {
			continue
		}
		_, decoded := deletionledger.DecodeSourceRetirementEntry(row.Canonical)
		if decoded != nil {
			// Frozen unknown source history stays opaque inside the retained
			// prefix, but can never be adopted as a new external extension.
			if row.Sequence > anchor.Primary.Sequence {
				return nil, ErrRecordLedgerFence
			}
			continue
		}
		matched := false
		for _, item := range pending {
			if item.found && item.entry.Sequence == row.Sequence {
				matched = true
				break
			}
		}
		if !matched {
			return nil, ErrRecordLedgerFence
		}
	}
	return pending, nil
}

func ledgerReadSourceWitnessed(dir int, item ledgerSourcePending) (ledgerSourceWitnessed, bool, error) {
	var receipt ledgerSourceWitnessed
	data, exists, err := fenceReadFile(dir, "source-witnessed-"+item.preview.Key+".json")
	if err != nil || !exists {
		return receipt, exists, err
	}
	encoded, err := json.Marshal(item.intent)
	if err != nil || fenceDecode(data, &receipt) != nil || receipt.Version != 1 || receipt.Key != item.preview.Key || receipt.IntentDigest != sha256.Sum256(encoded) || receipt.Phase != "handoff_pending" || !item.found || receipt.Entry.Validate() != nil || !bytes.Equal(receipt.Entry.Canonical, item.entry.Canonical) || receipt.Entry.EntryHash != item.entry.EntryHash || !ledgerSourceRequestEqual(receipt.Entry.Request, item.preview.Request) {
		return ledgerSourceWitnessed{}, false, ErrRecordLedgerFence
	}
	return receipt, true, nil
}

func (f *RecordLedgerFence) sourceAnchored(item ledgerSourcePending) (bool, error) {
	witnessed, exists, err := ledgerReadSourceWitnessed(f.control, item)
	if err != nil || !exists {
		return false, err
	}
	data, exists, err := fenceReadFile(f.control, "source-anchored-"+item.preview.Key+".json")
	if err != nil || !exists {
		return false, err
	}
	var receipt ledgerSourceAnchored
	encoded, err := json.Marshal(witnessed)
	if err != nil || fenceDecode(data, &receipt) != nil || receipt.Version != 1 || receipt.Key != item.preview.Key || receipt.WitnessedDigest != sha256.Sum256(encoded) || receipt.AnchorIndex >= uint64(len(f.anchors)) || f.anchors[receipt.AnchorIndex].Primary.Sequence < item.entry.Sequence || f.anchors[receipt.AnchorIndex].Witness.Sequence < item.entry.Sequence {
		return false, ErrRecordLedgerFence
	}
	return true, nil
}

// sourceRequestsSettled never treats an empty pair as proof that APP pending
// committed nothing. Controlled predecessor work may proceed only once every
// source intent is fully witnessed and externally anchored; ordinary record
// authority additionally requires the immutable C81 mutation handoff.
func (f *RecordLedgerFence) sourceRequestsSettled(ctx context.Context, requireHandoff bool) error {
	f.work.Lock()
	if err := f.checkAnchorsLocked(); err != nil {
		f.work.Unlock()
		return err
	}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		f.work.Unlock()
		return err
	}
	anchor := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, p, w, anchor)
	if err != nil {
		f.work.Unlock()
		return err
	}
	items, err := f.sourceJournalPair(primary, witness, anchor)
	if err == nil {
		for _, item := range items {
			anchored, e := f.sourceAnchored(item)
			if e != nil || !anchored || !item.found || item.entry.Sequence > witness.Head.Sequence {
				err = ErrRecordLedgerFence
				break
			}
		}
	}
	if err == nil && !requireHandoff {
		intents, e := ledgerReadIntents(f.control)
		if e != nil {
			err = e
		} else {
			for _, intent := range intents {
				if _, done, e := ledgerDone(f.control, intent); e != nil || !done {
					err = ErrRecordLedgerFence
					break
				}
			}
		}
	}
	f.work.Unlock()
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := f.VerifySourceRetirementProjection(ctx, item.preview.Request); err != nil {
			return err
		}
		if requireHandoff {
			if err := f.verifySourceRetirementHandoff(ctx, item.entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *RecordLedgerFence) verifyPendingSourceProjections(ctx context.Context) error {
	previews, err := ledgerReadSourcePreviews(f.control)
	if err != nil {
		return err
	}
	for _, preview := range previews {
		_, exists, err := ledgerReadSourceIntent(f.control, preview)
		if err != nil {
			return err
		}
		if exists {
			if err := f.VerifySourceRetirementProjection(ctx, preview.Request); err != nil {
				return err
			}
		}
	}
	return nil
}

// prepareSourceAdvance persists handoff responsibility before the anchor can
// move. The original APP intent remains immutable even after ledger completion.
func (f *RecordLedgerFence) prepareSourceAdvance(primary, witness recorddeletion.LedgerSnapshot, anchor ledgerAnchor) ([]ledgerSourcePending, error) {
	items, err := f.sourceJournalPair(primary, witness, anchor)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !item.found || item.entry.Sequence > witness.Head.Sequence {
			return nil, ErrRecordLedgerFence
		}
		encoded, err := json.Marshal(item.intent)
		if err != nil {
			return nil, ErrRecordLedgerFence
		}
		receipt := ledgerSourceWitnessed{Version: 1, Key: item.preview.Key, IntentDigest: sha256.Sum256(encoded), Entry: item.entry, Phase: "handoff_pending"}
		if _, err := ledgerPublishExact(f.control, "source-witnessed-"+item.preview.Key+".json", receipt); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (f *RecordLedgerFence) finishSourceAdvance(items []ledgerSourcePending) error {
	for _, item := range items {
		if done, err := f.sourceAnchored(item); err != nil {
			return err
		} else if done {
			continue
		}
		witnessed, exists, err := ledgerReadSourceWitnessed(f.control, item)
		if err != nil || !exists {
			return ErrRecordLedgerFence
		}
		encoded, err := json.Marshal(witnessed)
		if err != nil {
			return ErrRecordLedgerFence
		}
		receipt := ledgerSourceAnchored{Version: 1, Key: item.preview.Key, WitnessedDigest: sha256.Sum256(encoded), AnchorIndex: uint64(len(f.anchors) - 1)}
		if _, err := ledgerPublishExact(f.control, "source-anchored-"+item.preview.Key+".json", receipt); err != nil {
			return err
		}
	}
	return nil
}
