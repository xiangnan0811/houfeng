//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
)

// loadBootstrapCollectionSpec parses only the fixed bootstrap policy format.
// Unlike ebpf.LoadCollectionSpecFromReader, it never probes kernel BPF features.
func loadBootstrapCollectionSpec(rd io.ReaderAt) (*ebpf.CollectionSpec, error) {
	f, err := elf.NewFile(rd)
	if err != nil {
		return nil, errGuardProtocol
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB || f.ByteOrder != binary.LittleEndian || f.Type != elf.ET_REL || (f.Machine != elf.EM_BPF && f.Machine != elf.EM_NONE) || len(f.Sections) >= int(elf.SHN_LORESERVE) {
		return nil, errGuardProtocol
	}

	var mapsSec, licenseSec *elf.Section
	var mapsIndex elf.SectionIndex
	programSections := make(map[elf.SectionIndex]*elf.Section)
	programNames := map[string]bool{"lsm/bpf": true, "lsm/inode_rmdir": true, "lsm/inode_mkdir": true}
	seenPrograms := make(map[string]bool)
	seenText := false
	for i, sec := range f.Sections {
		switch {
		case sec.Name == ".maps":
			if mapsSec != nil || sec.Type != elf.SHT_PROGBITS || sec.Flags&elf.SHF_EXECINSTR != 0 || sec.Size > math.MaxUint32 {
				return nil, errGuardProtocol
			}
			mapsSec, mapsIndex = sec, elf.SectionIndex(i)
		case sec.Name == "license":
			if licenseSec != nil || sec.Type != elf.SHT_PROGBITS || sec.Flags&elf.SHF_EXECINSTR != 0 || sec.Size != 4 {
				return nil, errGuardProtocol
			}
			licenseSec = sec
		case sec.Name == ".text":
			if seenText || sec.Type != elf.SHT_PROGBITS || sec.Size != 0 {
				return nil, errGuardProtocol
			}
			seenText = true
		case programNames[sec.Name]:
			if seenPrograms[sec.Name] || sec.Type != elf.SHT_PROGBITS || sec.Flags&elf.SHF_EXECINSTR == 0 || sec.Size == 0 {
				return nil, errGuardProtocol
			}
			seenPrograms[sec.Name] = true
			programSections[elf.SectionIndex(i)] = sec
		case strings.HasPrefix(sec.Name, "maps"), strings.HasPrefix(sec.Name, "lsm/"):
			return nil, errGuardProtocol
		case bootstrapForbiddenDataSection(sec.Name) && sec.Size != 0:
			return nil, errGuardProtocol
		case sec.Flags&elf.SHF_EXECINSTR != 0:
			return nil, errGuardProtocol
		}
	}
	if mapsSec == nil || licenseSec == nil || !seenText || len(programSections) != len(programNames) {
		return nil, errGuardProtocol
	}
	license, err := licenseSec.Data()
	if err != nil || !bytes.Equal(license, []byte("GPL\x00")) {
		return nil, errGuardProtocol
	}
	symbols, err := f.Symbols()
	if err != nil || len(symbols) == 0 {
		return nil, errGuardProtocol
	}
	btfSpec, btfExtInfo, err := btf.LoadSpecAndExtInfosFromReader(rd)
	if err != nil || btfSpec == nil || btfExtInfo == nil {
		return nil, errGuardProtocol
	}
	// BTF can describe virtual sections even when no ELF section exists.
	for iter := btfSpec.Iterate(); iter.Next(); {
		if ds, ok := iter.Type.(*btf.Datasec); ok && bootstrapForbiddenDataSection(ds.Name) && (ds.Size != 0 || len(ds.Vars) != 0) {
			return nil, errGuardProtocol
		}
	}
	maps, err := bootstrapPolicyMaps(mapsSec, mapsIndex, symbols, btfSpec)
	if err != nil {
		return nil, errGuardProtocol
	}

	relocations := make(map[elf.SectionIndex]*elf.Section)
	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_REL && sec.Type != elf.SHT_RELA {
			continue
		}
		if uint64(sec.Info) >= uint64(len(f.Sections)) {
			return nil, errGuardProtocol
		}
		target := elf.SectionIndex(sec.Info)
		if targetSec := programSections[target]; targetSec != nil {
			if sec.Type != elf.SHT_REL || sec.Name != ".rel"+targetSec.Name || relocations[target] != nil || sec.Entsize != 16 || sec.Size%16 != 0 {
				return nil, errGuardProtocol
			}
			relocations[target] = sec
		} else if target == mapsIndex || f.Sections[target].Flags&elf.SHF_ALLOC != 0 || f.Sections[target].Flags&elf.SHF_EXECINSTR != 0 {
			return nil, errGuardProtocol
		}
	}
	programs := make(map[string]*ebpf.ProgramSpec, len(programSections))
	for index, sec := range programSections {
		program, err := bootstrapPolicyProgram(sec, index, relocations[index], mapsIndex, symbols, maps, btfExtInfo)
		if err != nil || programs[program.Name] != nil {
			return nil, errGuardProtocol
		}
		programs[program.Name] = program
	}
	return &ebpf.CollectionSpec{Maps: maps, Programs: programs, Types: btfSpec, ByteOrder: binary.LittleEndian}, nil
}

func bootstrapForbiddenDataSection(name string) bool {
	return strings.HasPrefix(name, ".data") || strings.HasPrefix(name, ".rodata") || strings.HasPrefix(name, ".bss") || name == ".kconfig" || name == ".ksyms"
}

func bootstrapPolicyName(name string) bool {
	if len(name) == 0 || len(name) > 15 {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func bootstrapUintFromBTF(typ btf.Type) (uint32, bool) {
	ptr, ok := typ.(*btf.Pointer)
	if !ok {
		return 0, false
	}
	arr, ok := ptr.Target.(*btf.Array)
	if !ok {
		return 0, false
	}
	return arr.Nelems, true
}

func bootstrapPolicyMaps(sec *elf.Section, index elf.SectionIndex, symbols []elf.Symbol, spec *btf.Spec) (map[string]*ebpf.MapSpec, error) {
	var ds *btf.Datasec
	if err := spec.TypeByName(".maps", &ds); err != nil || ds == nil {
		return nil, errGuardProtocol
	}
	raw, err := sec.Data()
	if err != nil || uint64(len(raw)) != sec.Size || uint64(ds.Size) != sec.Size || !guardZero(raw) {
		return nil, errGuardProtocol
	}
	maps := make(map[string]*ebpf.MapSpec, len(ds.Vars))
	var offset uint64
	for _, vs := range ds.Vars {
		v, ok := vs.Type.(*btf.Var)
		if !ok || v.Linkage != btf.GlobalVar || !bootstrapPolicyName(v.Name) || maps[v.Name] != nil || uint64(vs.Offset) != offset || vs.Size == 0 {
			return nil, errGuardProtocol
		}
		offset += uint64(vs.Size)
		if offset > uint64(len(raw)) {
			return nil, errGuardProtocol
		}
		def, ok := btf.UnderlyingType(v.Type).(*btf.Struct)
		if !ok || def.Size != vs.Size {
			return nil, errGuardProtocol
		}
		matches := 0
		for _, sym := range symbols {
			if sym.Name == v.Name && sym.Section == index {
				if elf.ST_BIND(sym.Info) != elf.STB_GLOBAL || (elf.ST_TYPE(sym.Info) != elf.STT_OBJECT && elf.ST_TYPE(sym.Info) != elf.STT_NOTYPE) || sym.Value != uint64(vs.Offset) || (sym.Size != 0 && sym.Size != uint64(vs.Size)) {
					return nil, errGuardProtocol
				}
				matches++
			}
		}
		if matches != 1 {
			return nil, errGuardProtocol
		}
		m := &ebpf.MapSpec{Name: v.Name, Pinning: ebpf.PinNone}
		seen := make(map[string]bool, len(def.Members))
		for _, member := range def.Members {
			if seen[member.Name] {
				return nil, errGuardProtocol
			}
			seen[member.Name] = true
			switch member.Name {
			case "type", "map_flags", "max_entries":
				n, ok := bootstrapUintFromBTF(member.Type)
				if !ok {
					return nil, errGuardProtocol
				}
				switch member.Name {
				case "type":
					m.Type = ebpf.MapType(n)
				case "map_flags":
					m.Flags = n
				case "max_entries":
					m.MaxEntries = n
				}
			case "key", "value":
				ptr, ok := member.Type.(*btf.Pointer)
				if !ok || ptr.Target == nil {
					return nil, errGuardProtocol
				}
				size, err := btf.Sizeof(ptr.Target)
				if err != nil || size <= 0 || uint64(size) > math.MaxUint32 {
					return nil, errGuardProtocol
				}
				if member.Name == "key" {
					m.Key, m.KeySize = ptr.Target, uint32(size)
				} else {
					m.Value, m.ValueSize = ptr.Target, uint32(size)
				}
			default:
				return nil, errGuardProtocol
			}
		}
		if !seen["type"] || !seen["key"] || !seen["value"] {
			return nil, errGuardProtocol
		}
		maps[v.Name] = m
	}
	if offset != uint64(len(raw)) {
		return nil, errGuardProtocol
	}
	return maps, nil
}

func bootstrapPolicyProgram(sec *elf.Section, index elf.SectionIndex, relSec *elf.Section, mapsIndex elf.SectionIndex, symbols []elf.Symbol, maps map[string]*ebpf.MapSpec, ext *btf.ExtInfos) (*ebpf.ProgramSpec, error) {
	attachTo, ok := strings.CutPrefix(sec.Name, "lsm/")
	if !ok || attachTo == "" || relSec == nil {
		return nil, errGuardProtocol
	}
	var name string
	for _, sym := range symbols {
		if sym.Section != index || elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		if name != "" || elf.ST_BIND(sym.Info) != elf.STB_GLOBAL || sym.Value != 0 || !bootstrapPolicyName(sym.Name) {
			return nil, errGuardProtocol
		}
		name = sym.Name
	}
	if name == "" {
		return nil, errGuardProtocol
	}
	var insns asm.Instructions
	if err := insns.Unmarshal(bufio.NewReader(sec.Open()), binary.LittleEndian); err != nil || len(insns) == 0 || insns.Size() != sec.Size {
		return nil, errGuardProtocol
	}
	insns[0] = insns[0].WithSymbol(name)
	relReader := bufio.NewReader(relSec.Open())
	pending := make(map[uint64]string)
	for off := uint64(0); off < relSec.Size; off += 16 {
		var rel elf.Rel64
		if binary.Read(relReader, binary.LittleEndian, &rel) != nil || elf.R_TYPE64(rel.Info) != 1 || pending[rel.Off] != "" {
			return nil, errGuardProtocol
		}
		symNo := elf.R_SYM64(rel.Info)
		if symNo == 0 || uint64(symNo) > uint64(len(symbols)) {
			return nil, errGuardProtocol
		}
		sym := symbols[symNo-1]
		if sym.Section != mapsIndex || elf.ST_BIND(sym.Info) != elf.STB_GLOBAL || (elf.ST_TYPE(sym.Info) != elf.STT_OBJECT && elf.ST_TYPE(sym.Info) != elf.STT_NOTYPE) || maps[sym.Name] == nil {
			return nil, errGuardProtocol
		}
		pending[rel.Off] = sym.Name
	}
	iter := insns.Iterate()
	for iter.Next() {
		offset := iter.Offset.Bytes()
		mapName, ok := pending[offset]
		if !ok {
			continue
		}
		if iter.Ins.OpCode != asm.LoadImmOp(asm.DWord) || iter.Ins.Src != 0 || iter.Ins.Offset != 0 || iter.Ins.Constant != 0 {
			return nil, errGuardProtocol
		}
		iter.Ins.Src = asm.PseudoMapFD
		*iter.Ins = iter.Ins.WithReference(mapName)
		delete(pending, offset)
	}
	if len(pending) != 0 {
		return nil, errGuardProtocol
	}
	ext.Assign(insns, sec.Name)
	if fn := btf.FuncMetadata(&insns[0]); fn == nil || fn.Name != name {
		return nil, errGuardProtocol
	}
	return &ebpf.ProgramSpec{Name: name, Type: ebpf.LSM, AttachType: ebpf.AttachLSMMac, AttachTo: attachTo, SectionName: sec.Name, License: "GPL", Instructions: insns, ByteOrder: binary.LittleEndian}, nil
}

// bootstrapPolicyImage is an in-memory relocation of the fixed, verified ELF.
// It performs no BPF syscall. The separate observed loader must record every
// syscall, including BTF_LOAD, before using this image to establish any gate.
type bootstrapPolicyImage struct {
	btfBytes []byte
	maps     map[string]bootstrapMapImage
	programs map[string]bootstrapProgramImage
}

type bootstrapMapImage struct {
	spec               *ebpf.MapSpec
	keyType, valueType btf.TypeID
}

type bootstrapProgramImage struct {
	spec      *ebpf.ProgramSpec
	attachBTF uint32
	funcInfo  []byte
}

func prepareBootstrapPolicy(spec *ebpf.CollectionSpec, kernel *btf.Spec) (*bootstrapPolicyImage, error) {
	if err := checkPolicySpec(spec); err != nil {
		return nil, err
	}
	if kernel == nil {
		return nil, errGuardProtocol
	}
	builder := &btf.Builder{}
	out := &bootstrapPolicyImage{maps: make(map[string]bootstrapMapImage), programs: make(map[string]bootstrapProgramImage)}
	for _, name := range []string{"broker_token", "guard_liveness", "realm_targets", "retire_tasks"} {
		m := spec.Maps[name]
		if m.Key == nil || m.Value == nil {
			return nil, errGuardProtocol
		}
		key, err := builder.Add(m.Key)
		if err != nil {
			return nil, err
		}
		value, err := builder.Add(m.Value)
		if err != nil {
			return nil, err
		}
		if key == 0 || value == 0 {
			return nil, errGuardProtocol
		}
		out.maps[name] = bootstrapMapImage{spec: m.Copy(), keyType: key, valueType: value}
	}
	for _, name := range []string{"g2_bpf", "g2_inode_rmdir", "g2_inode_mkdir"} {
		p := spec.Programs[name].Copy()
		var fn *btf.Func
		if err := kernel.TypeByName("bpf_lsm_"+p.AttachTo, &fn); err != nil {
			return nil, err
		}
		attach, err := kernel.TypeID(fn)
		if err != nil || attach == 0 {
			return nil, errGuardProtocol
		}
		var relos []*btf.CORERelocation
		var targets []*asm.Instruction
		iter := p.Instructions.Iterate()
		for iter.Next() {
			if relocation := btf.CORERelocationMetadata(iter.Ins); relocation != nil {
				relos = append(relos, relocation)
				targets = append(targets, iter.Ins)
			}
		}
		fixups, err := btf.CORERelocate(relos, []*btf.Spec{kernel}, binary.LittleEndian, builder.Add)
		if err != nil || len(fixups) != len(targets) {
			return nil, fmt.Errorf("BPF_AUTH_INCOMPATIBLE: kernel CO-RE relocation: %w", err)
		}
		for i := range fixups {
			if err = fixups[i].Apply(targets[i]); err != nil {
				return nil, err
			}
		}
		var funcs bytes.Buffer
		iter = p.Instructions.Iterate()
		for iter.Next() {
			f := btf.FuncMetadata(iter.Ins)
			if f == nil {
				continue
			}
			id, e := builder.Add(f)
			if e != nil || id == 0 {
				return nil, errGuardProtocol
			}
			if uint64(iter.Offset) > uint64(^uint32(0)) {
				return nil, errGuardProtocol
			}
			var entry [8]byte
			binary.LittleEndian.PutUint32(entry[0:4], uint32(iter.Offset))
			binary.LittleEndian.PutUint32(entry[4:8], uint32(id))
			funcs.Write(entry[:])
		}
		if funcs.Len() == 0 || binary.LittleEndian.Uint32(funcs.Bytes()[0:4]) != 0 {
			return nil, errGuardProtocol
		}
		out.programs[name] = bootstrapProgramImage{spec: p, attachBTF: uint32(attach), funcInfo: funcs.Bytes()}
	}
	var err error
	out.btfBytes, err = builder.Marshal(nil, &btf.MarshalOptions{Order: binary.LittleEndian, PreventNoTypeFound: true})
	if err != nil {
		return nil, err
	}
	if len(out.btfBytes) == 0 || len(out.btfBytes)*2+1024 > maxControlFrame {
		return nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: bootstrap BTF evidence exceeds frame")
	}
	return out, nil
}

type bootstrapMapHandle struct {
	fd     int
	cookie uint64
}

func (p bootstrapProgramImage) kernelInstructions(maps map[string]bootstrapMapHandle) ([]byte, []any, error) {
	insns := p.spec.Copy().Instructions
	var inputs []any
	var offsets []int
	var descriptors []uint32
	iter := insns.Iterate()
	for iter.Next() {
		if iter.Ins.OpCode == asm.LoadImmOp(asm.DWord) && iter.Ins.Src != 0 && iter.Ins.Src != asm.PseudoMapFD {
			return nil, nil, errGuardProtocol
		}
		if !iter.Ins.IsLoadFromMap() {
			continue
		}
		if iter.Ins.Src != asm.PseudoMapFD {
			return nil, nil, errGuardProtocol
		}
		name := iter.Ins.Reference()
		item, ok := maps[name]
		if !ok || item.fd < 0 || int64(item.fd) > 0x7fffffff || item.cookie == 0 {
			return nil, nil, errGuardProtocol
		}
		if err := iter.Ins.RewriteMapPtr(item.fd); err != nil {
			return nil, nil, err
		}
		offset := int(iter.Offset) * 8
		offsets = append(offsets, offset)
		descriptors = append(descriptors, uint32(item.fd))
		inputs = append(inputs, map[string]any{"field": fmt.Sprintf("insns[%d].map_fd", offset), "kind": "map", "cookie": jsonU64(item.cookie), "requester_fd": jsonU64(uint64(item.fd))})
	}
	var raw bytes.Buffer
	if err := insns.Marshal(&raw, binary.LittleEndian); err != nil {
		return nil, nil, err
	}
	if raw.Len() == 0 || raw.Len()%8 != 0 || raw.Len()/8 > 4096 {
		return nil, nil, errGuardProtocol
	}
	code := raw.Bytes()
	for i, offset := range offsets {
		if offset < 0 || offset+16 > len(code) || code[offset] != 0x18 || code[offset+1]>>4 != 1 || code[offset+2] != 0 || code[offset+3] != 0 || binary.LittleEndian.Uint32(code[offset+4:offset+8]) != descriptors[i] || !guardZero(code[offset+8:offset+16]) {
			return nil, nil, errGuardProtocol
		}
	}
	return code, inputs, nil
}
