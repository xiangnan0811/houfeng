//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type bootstrapProgramHandle struct {
	fd               int
	cookie, kernelID uint64
	attachBTF        uint32
}

func loadBootstrapProgram(g guardDevice, rec *bootstrapRecorder, cursor *uint64, image bootstrapProgramImage, btfFD int, maps map[string]bootstrapMapHandle) (_ bootstrapProgramHandle, err error) {
	var empty bootstrapProgramHandle
	if rec == nil || cursor == nil || image.spec == nil || btfFD < 0 || int64(btfFD) > math.MaxInt32 || image.attachBTF == 0 || len(image.funcInfo) == 0 || len(image.funcInfo)%8 != 0 {
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
	instructions, inputs, err := image.kernelInstructions(maps)
	if err != nil {
		return empty, err
	}
	if inputs == nil {
		inputs = []any{}
	}
	name := image.spec.Name
	if len(name) == 0 || len(name) > 15 || image.spec.License != "GPL" {
		return empty, errGuardProtocol
	}
	license := []byte("GPL\x00")
	outer := make([]byte, 144)
	guardPut32(outer, 0, uint32(image.spec.Type))
	guardPut32(outer, 4, uint32(len(instructions)/8))
	copy(outer[48:64], name)
	guardPut32(outer, 68, uint32(image.spec.AttachType))
	guardPut32(outer, 72, uint32(btfFD))
	guardPut32(outer, 76, 8)
	guardPut32(outer, 88, uint32(len(image.funcInfo)/8))
	guardPut32(outer, 108, image.attachBTF)
	programHex := hex.EncodeToString(instructions)
	funcInfoHex := hex.EncodeToString(image.funcInfo)
	possible := map[string]any{"object_cookie": jsonU64(math.MaxUint64), "log_true_size": jsonU64(math.MaxUint32), "func_info_hex": funcInfoHex}
	t, err := rec.prepare("PROG_LOAD", outer, programHex, "GPL", inputs, possible)
	if err != nil {
		return empty, err
	}
	if err = rec.transition(t, "committed", nil, nil); err != nil {
		return empty, err
	}
	actual := append([]byte(nil), outer...)
	guardPut64(actual, 8, uint64(uintptr(unsafe.Pointer(&instructions[0]))))
	guardPut64(actual, 16, uint64(uintptr(unsafe.Pointer(&license[0]))))
	guardPut64(actual, 80, uint64(uintptr(unsafe.Pointer(&image.funcInfo[0]))))
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_LOAD, uintptr(unsafe.Pointer(&actual[0])), uintptr(len(actual)))
	runtime.KeepAlive(actual)
	runtime.KeepAlive(instructions)
	runtime.KeepAlive(license)
	runtime.KeepAlive(image.funcInfo)
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
	observed := &bootstrapPublication{rec: rec, tx: t, class: "program", typ: uint32(image.spec.Type), program: programHex, failed: errno != 0}
	if _, err = bootstrapDrainObserved(g, rec, cursor, observed.observe); err != nil {
		return empty, err
	}
	publication := observed.publication
	if errno == 0 && (fd > math.MaxInt32 || publication.Sequence == 0) {
		return empty, errGuardProtocol
	}
	var objectCookie any
	if publication.Sequence != 0 {
		objectCookie = jsonU64(publication.ObjectCookie)
	}
	if publication.Sequence != 0 {
		attrs, ok := observed.snapshot["attributes"].(map[string]any)
		if !ok || attrs["name"] != name || !observedU32(attrs, "attach_btf_id", image.attachBTF) {
			return empty, errGuardProtocol
		}
	}
	output := map[string]any{"object_cookie": objectCookie, "log_true_size": jsonU64(uint64(guardU32(actual, 140))), "func_info_hex": funcInfoHex}
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
		return empty, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap PROG_LOAD: %w", errno)
	}
	id, attrs, err := observeBootstrapProgramInfo(rec, int(fd), publication.ObjectCookie, uint32(image.spec.Type))
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(observed.snapshot["attributes"], attrs) {
		return empty, errGuardProtocol
	}
	if uint64(id) != publication.KernelID || attrs["name"] != name || !observedU32(attrs, "attach_btf_id", image.attachBTF) {
		return empty, errGuardProtocol
	}
	if err = rec.transition(t, "response_settled", jsonI64(signed), output); err != nil {
		return empty, err
	}
	if err = rec.transition(t, "retired", jsonI64(signed), output); err != nil {
		return empty, err
	}
	keep = true
	return bootstrapProgramHandle{fd: int(fd), cookie: publication.ObjectCookie, kernelID: publication.KernelID, attachBTF: image.attachBTF}, nil
}
