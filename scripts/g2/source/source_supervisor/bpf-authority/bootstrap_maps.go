//go:build linux && amd64 && !cgo

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func observedU32(attrs map[string]any, key string, want uint32) bool {
	v, ok := exactNumber(attrs[key])
	return ok && v == uint64(want)
}

// createBootstrapMap binds a real map publication to its observed returned FD
// and independent object-info result. No initial map authorization is inferred
// from the ELF spec or from a reused numeric kernel ID.
func createBootstrapMap(g guardDevice, rec *bootstrapRecorder, cursor *uint64, image bootstrapMapImage, btfFD int) (_ bootstrapMapHandle, err error) {
	var empty bootstrapMapHandle
	if rec == nil || cursor == nil || image.spec == nil || btfFD < 0 || int64(btfFD) > math.MaxInt32 || image.keyType == 0 || image.valueType == 0 {
		return empty, errGuardProtocol
	}
	prior, err := bootstrapDrainRecords(g, rec, cursor)
	if err != nil {
		return empty, err
	}
	for _, event := range prior {
		if event.Kind == 1 || event.Kind == 3 {
			return empty, errGuardProtocol
		}
	}
	spec := image.spec
	if len(spec.Name) == 0 || len(spec.Name) > 15 {
		return empty, errGuardProtocol
	}
	outer := make([]byte, 72)
	guardPut32(outer, 0, uint32(spec.Type))
	guardPut32(outer, 4, spec.KeySize)
	guardPut32(outer, 8, spec.ValueSize)
	guardPut32(outer, 12, spec.MaxEntries)
	guardPut32(outer, 16, spec.Flags)
	copy(outer[28:44], spec.Name)
	guardPut32(outer, 48, uint32(btfFD))
	guardPut32(outer, 52, uint32(image.keyType))
	guardPut32(outer, 56, uint32(image.valueType))
	possible := map[string]any{"object_cookie": jsonU64(math.MaxUint64)}
	t, err := rec.prepare("MAP_CREATE", outer, nil, nil, []any{}, possible)
	if err != nil {
		return empty, err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return empty, err
	}
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_MAP_CREATE, uintptr(unsafe.Pointer(&outer[0])), uintptr(len(outer)))
	runtime.KeepAlive(outer)
	signed := int64(fd)
	if errno != 0 {
		signed = -int64(errno)
	}
	keep := false
	defer func() {
		if !keep && errno == 0 && fd <= math.MaxInt32 {
			_ = unix.Close(int(fd))
		}
	}()
	observed := &bootstrapPublication{rec: rec, tx: t, class: "map", typ: uint32(spec.Type), failed: errno != 0}
	if _, err = bootstrapDrainObserved(g, rec, cursor, observed.observe); err != nil {
		return empty, err
	}
	publication := observed.publication
	if errno == 0 && (fd > math.MaxInt32 || publication.Sequence == 0) {
		return empty, errGuardProtocol
	}
	var output any
	if publication.Sequence != 0 {
		output = map[string]any{"object_cookie": jsonU64(publication.ObjectCookie)}
	}
	if publication.Sequence != 0 {
		attrs, ok := observed.snapshot["attributes"].(map[string]any)
		if !ok || attrs["name"] != spec.Name || !observedU32(attrs, "key_size", spec.KeySize) || !observedU32(attrs, "value_size", spec.ValueSize) || !observedU32(attrs, "max_entries", spec.MaxEntries) || !observedU32(attrs, "map_flags", spec.Flags) || !observedU32(attrs, "btf_key_type_id", uint32(image.keyType)) || !observedU32(attrs, "btf_value_type_id", uint32(image.valueType)) || attrs["map_extra"] != json.Number("0") {
			return empty, errGuardProtocol
		}
	}
	if err = rec.transition(t, "effect_observed", jsonI64(signed), output); err != nil {
		return empty, err
	}
	if errno != 0 {
		if publication.Sequence != 0 && observed.removal.Sequence == 0 {
			if err = observed.waitRemoval(g, cursor); err != nil {
				return empty, err
			}
		}
		if err = rec.transition(t, "response_settled", jsonI64(signed), output); err != nil {
			return empty, err
		}
		if err = rec.transition(t, "retired", jsonI64(signed), output); err != nil {
			return empty, err
		}
		return empty, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap MAP_CREATE: %w", errno)
	}
	actualID, attrs, err := observeBootstrapMapInfo(rec, int(fd), publication.ObjectCookie, uint32(spec.Type))
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(observed.snapshot["attributes"], attrs) {
		return empty, errGuardProtocol
	}
	if uint64(actualID) != publication.KernelID || attrs["name"] != spec.Name || !observedU32(attrs, "key_size", spec.KeySize) || !observedU32(attrs, "value_size", spec.ValueSize) || !observedU32(attrs, "max_entries", spec.MaxEntries) || !observedU32(attrs, "map_flags", spec.Flags) || !observedU32(attrs, "btf_key_type_id", uint32(image.keyType)) || !observedU32(attrs, "btf_value_type_id", uint32(image.valueType)) || attrs["map_extra"] != json.Number("0") {
		return empty, errGuardProtocol
	}
	if err = rec.transition(t, "response_settled", jsonI64(signed), output); err != nil {
		return empty, err
	}
	if err = rec.transition(t, "retired", jsonI64(signed), output); err != nil {
		return empty, err
	}
	keep = true
	return bootstrapMapHandle{fd: int(fd), cookie: publication.ObjectCookie}, nil
}
