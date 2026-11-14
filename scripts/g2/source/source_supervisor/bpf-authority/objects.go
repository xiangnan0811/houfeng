//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"reflect"
)

type objectIncarnation struct {
	cookie, id, ownerTransaction, publicationSeq uint64
	removalSeq                                   uint64
	class                                        string
	objectType                                   uint32
	attributes                                   map[string]any
	programBytes                                 any
	removalAuthorized                            bool
	state                                        string
}

type objectTracker struct {
	transcript *authorityTranscript
	byCookie   map[uint64]*objectIncarnation
	latestByID map[objectID]*objectIncarnation
}

type objectID struct {
	class string
	id    uint64
}

func newObjectTracker(t *authorityTranscript) *objectTracker {
	return &objectTracker{transcript: t, byCookie: make(map[uint64]*objectIncarnation), latestByID: make(map[objectID]*objectIncarnation)}
}

func validObjectAttributes(class string, attributes map[string]any) bool {
	var u32 []string
	switch class {
	case "program":
		if !exactKeys(attributes, "name", "tag", "load_time", "created_by_uid", "translated_insn_count", "verified_insns", "attach_btf_id") {
			return false
		}
		tag, ok := attributes["tag"].(string)
		if !ok || len(tag) != 16 {
			return false
		}
		for _, c := range tag {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		if _, ok = exactNumber(attributes["load_time"]); !ok {
			return false
		}
		u32 = []string{"created_by_uid", "translated_insn_count", "verified_insns", "attach_btf_id"}
	case "map":
		if !exactKeys(attributes, "name", "key_size", "value_size", "max_entries", "map_flags", "btf_id", "btf_key_type_id", "btf_value_type_id", "map_extra") {
			return false
		}
		if _, ok := exactNumber(attributes["map_extra"]); !ok {
			return false
		}
		u32 = []string{"key_size", "value_size", "max_entries", "map_flags", "btf_id", "btf_key_type_id", "btf_value_type_id"}
	default:
		return false
	}
	name, ok := attributes["name"].(string)
	if !ok || len(name) > 16 {
		return false
	}
	for _, c := range name {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	for _, key := range u32 {
		n, ok := exactNumber(attributes[key])
		if !ok || n > 0xffffffff {
			return false
		}
	}
	return true
}

func (t *objectTracker) emit(o *objectIncarnation) error {
	var removal any
	if o.removalSeq != 0 {
		removal = jsonU64(o.removalSeq)
	}
	_, _, err := t.transcript.append("object", map[string]any{
		"cookie": jsonU64(o.cookie), "class": o.class, "kernel_id": jsonU64(o.id), "owner_transaction": jsonU64(o.ownerTransaction),
		"type": jsonU64(uint64(o.objectType)), "attributes": o.attributes, "bytes": o.programBytes,
		"publication_seq": jsonU64(o.publicationSeq), "attribute_source": map[string]any{"kind": "native_publication", "guard_sequence": jsonU64(o.publicationSeq)}, "removal_seq": removal, "state": o.state,
	})
	return err
}

// publish consumes the guard's actual publication snapshot. Input bytecode is
// the captured input, independent of the kernel's translated program.
func (t *objectTracker) publish(r guardRecord, owner uint64, typ uint32, attributes map[string]any, programHex string) error {
	if t == nil || t.transcript == nil || r.Sequence == 0 || r.ObjectCookie == 0 || r.KernelID == 0 || owner == 0 || len(t.byCookie) >= 384 {
		return errGuardProtocol
	}
	class := "program"
	if r.Kind == 3 {
		class = "map"
	} else if r.Kind != 1 {
		return errGuardProtocol
	}
	snapshot, err := r.objectSnapshot()
	if err != nil || snapshot == nil || snapshot["type"] != jsonU64(uint64(typ)) || !reflect.DeepEqual(snapshot["attributes"], attributes) {
		return errGuardProtocol
	}
	if _, exists := t.byCookie[r.ObjectCookie]; exists || !validObjectAttributes(class, attributes) {
		return errGuardProtocol
	}
	var code any
	if class == "program" {
		if len(programHex) == 0 || len(programHex) > 4096*16 || len(programHex)%16 != 0 {
			return errGuardProtocol
		}
		decoded, err := hex.DecodeString(programHex)
		if err != nil || hex.EncodeToString(decoded) != programHex {
			return errGuardProtocol
		}
		code = programHex
	} else if programHex != "" {
		return errGuardProtocol
	}
	id := objectID{class, r.KernelID}
	if old := t.latestByID[id]; old != nil && old.removalSeq == 0 {
		return errGuardProtocol
	}
	object := &objectIncarnation{cookie: r.ObjectCookie, id: r.KernelID, ownerTransaction: owner, publicationSeq: r.Sequence, class: class, objectType: typ, attributes: attributes, programBytes: code, state: "live"}
	if err := t.emit(object); err != nil {
		return err
	}
	t.byCookie[object.cookie] = object
	t.latestByID[id] = object
	return nil
}

// authorizeRemoval is called only after an effect's explicit cleanup or
// preauthorized last-reference transition has actually been committed.
func (t *objectTracker) authorizeRemoval(cookie uint64) error {
	if t == nil {
		return errGuardProtocol
	}
	o := t.byCookie[cookie]
	if o == nil || o.removalSeq != 0 || o.removalAuthorized {
		return errGuardProtocol
	}
	o.removalAuthorized = true
	return nil
}

func (t *objectTracker) removed(r guardRecord) error {
	if t == nil || t.transcript == nil || r.Sequence == 0 {
		return errGuardProtocol
	}
	o := t.byCookie[r.ObjectCookie]
	if o == nil || !o.removalAuthorized || o.removalSeq != 0 || o.id != r.KernelID || r.Sequence <= o.publicationSeq {
		return errGuardProtocol
	}
	if o.class == "program" && r.Kind != 2 || o.class == "map" && r.Kind != 4 {
		return errGuardProtocol
	}
	o.removalSeq = r.Sequence
	o.state = "id_removed"
	if err := t.emit(o); err != nil {
		return err
	}
	key := objectID{o.class, o.id}
	if t.latestByID[key] == o {
		delete(t.latestByID, key)
	}
	return nil
}
