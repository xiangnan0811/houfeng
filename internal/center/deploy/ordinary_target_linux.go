//go:build linux

package deploy

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"io"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// ordinaryStaticELF validates the bytes actually extracted from the pinned
// image, rather than assuming the Dockerfile's build flags were honored.
func ordinaryStaticELF(reader io.ReaderAt, size int64, arch string) error {
	if size <= 0 || size > 256<<20 {
		return errOrdinaryUpgrade
	}
	file, err := elf.NewFile(io.NewSectionReader(reader, 0, size))
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer file.Close()
	machine := elf.EM_NONE
	switch arch {
	case "amd64":
		machine = elf.EM_X86_64
	case "arm64":
		machine = elf.EM_AARCH64
	default:
		return errOrdinaryUpgrade
	}
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.ByteOrder != binary.LittleEndian || file.Machine != machine || (file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN) {
		return errOrdinaryUpgrade
	}
	entryExecutable := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP || program.Type == elf.PT_DYNAMIC || (program.Type == elf.PT_GNU_STACK && program.Flags&elf.PF_X != 0) {
			return errOrdinaryUpgrade
		}
		if program.Type != elf.PT_LOAD {
			continue
		}
		if program.Filesz > program.Memsz || program.Off > uint64(size) || program.Filesz > uint64(size)-program.Off || program.Vaddr+program.Memsz < program.Vaddr {
			return errOrdinaryUpgrade
		}
		if program.Flags&elf.PF_X != 0 && file.Entry >= program.Vaddr && file.Entry-program.Vaddr < program.Memsz {
			entryExecutable = true
		}
	}
	if !entryExecutable {
		return errOrdinaryUpgrade
	}
	return nil
}

// ordinaryTargetExecutable hashes a root-owned, non-writable installed file.
// The caller must additionally pin its parent path and image provenance.
func ordinaryTargetExecutable(file *os.File) ([32]byte, error) {
	var digest [32]byte
	if file == nil {
		return digest, errOrdinaryUpgrade
	}
	fd := int(file.Fd())
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o550 || before.Uid != 0 || before.Gid != composeLaunchRuntimeGID || before.Nlink != 1 || before.Size <= 0 || before.Size > 256<<20 {
		return digest, errOrdinaryUpgrade
	}
	if _, err := unix.Fgetxattr(fd, "security.capability", nil); err != unix.ENODATA && err != unix.ENOTSUP {
		return digest, errOrdinaryUpgrade
	}
	if ordinaryStaticELF(file, before.Size, runtime.GOARCH) != nil {
		return digest, errOrdinaryUpgrade
	}
	hash := sha256.New()
	if n, err := io.Copy(hash, io.NewSectionReader(file, 0, before.Size)); err != nil || n != before.Size || unix.Fstat(fd, &after) != nil {
		return digest, errOrdinaryUpgrade
	}
	before.Atim = after.Atim
	if before != after {
		return digest, errOrdinaryUpgrade
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
