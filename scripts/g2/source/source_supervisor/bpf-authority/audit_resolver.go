//go:build linux && amd64 && !cgo

package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

type auditedTransaction struct {
	ID, ExecutorCookie uint64
	Command            string
	Number             uint64
	Result             int64
}

type auditedObject struct {
	Cookie, LoadTransactionID, UnloadTransactionID uint64
	ProgramID, PublicationSequence                 uint64
	LoadAuthorized, UnloadAuthorized               bool
}

type serialAuditIdentity struct {
	TaskCookie, TransactionID uint64
	Command                   string
	RawSyscall                bool
	Number                    uint64
	Result                    int64
}

// brokerAuditResolver accepts only facts already attached to real held broker
// transactions and guard-observed object incarnations. An audit TGID alone
// cannot establish the actual nonleader thread that performed a syscall.
type brokerAuditResolver struct {
	brokerPID    uint64
	brokerCookie uint64
	transactions []auditedTransaction
	objects      map[uint64]auditedObject
	serials      map[auditEventKey]serialAuditIdentity
}

func newBrokerAuditResolver(pid, cookie uint64) (*brokerAuditResolver, error) {
	if pid == 0 || cookie == 0 {
		return nil, errGuardProtocol
	}
	return &brokerAuditResolver{brokerPID: pid, brokerCookie: cookie, objects: make(map[uint64]auditedObject), serials: make(map[auditEventKey]serialAuditIdentity)}, nil
}

func bpfCommand(number uint64) string {
	switch number {
	case unix.BPF_MAP_CREATE:
		return "MAP_CREATE"
	case unix.BPF_MAP_LOOKUP_ELEM:
		return "MAP_LOOKUP_ELEM"
	case unix.BPF_MAP_UPDATE_ELEM:
		return "MAP_UPDATE_ELEM"
	case unix.BPF_MAP_DELETE_ELEM:
		return "MAP_DELETE_ELEM"
	case unix.BPF_MAP_FREEZE:
		return "MAP_FREEZE"
	case unix.BPF_PROG_LOAD:
		return "PROG_LOAD"
	case unix.BPF_PROG_QUERY:
		return "PROG_QUERY"
	case unix.BPF_PROG_GET_FD_BY_ID:
		return "PROG_GET_FD_BY_ID"
	case unix.BPF_OBJ_GET_INFO_BY_FD:
		return "OBJ_GET_INFO_BY_FD"
	case unix.BPF_PROG_ATTACH:
		return "PROG_ATTACH"
	case unix.BPF_PROG_DETACH:
		return "PROG_DETACH"
	case unix.BPF_LINK_DETACH:
		return "LINK_DETACH"
	// These commands are used only during sealed bootstrap. Mapping their
	// observed syscall numbers here does not grant runtime authorization.
	case unix.BPF_BTF_LOAD:
		return "BTF_LOAD"
	case unix.BPF_LINK_CREATE:
		return "LINK_CREATE"
	default:
		return ""
	}
}

func (r *brokerAuditResolver) recordTransaction(tx auditedTransaction) error {
	if r == nil || tx.ID == 0 || tx.ExecutorCookie != r.brokerCookie || tx.Command == "" || tx.Command != bpfCommand(tx.Number) || len(r.transactions) >= 4096 {
		return errGuardProtocol
	}
	r.transactions = append(r.transactions, tx)
	return nil
}

func (r *brokerAuditResolver) recordObject(object auditedObject) error {
	if r == nil || object.Cookie == 0 || object.LoadTransactionID == 0 || object.ProgramID == 0 || object.PublicationSequence == 0 || object.UnloadAuthorized || object.UnloadTransactionID != 0 || len(r.objects) >= 256 {
		return errGuardProtocol
	}
	if _, exists := r.objects[object.Cookie]; exists {
		return errGuardProtocol
	}
	r.objects[object.Cookie] = object
	return nil
}

func (r *brokerAuditResolver) auditIncarnation(cookie, id uint64) (uint64, bool, error) {
	if r == nil {
		return 0, false, errGuardProtocol
	}
	object, ok := r.objects[cookie]
	if !ok || object.ProgramID != id || object.PublicationSequence == 0 {
		return 0, false, errGuardProtocol
	}
	return object.PublicationSequence, object.UnloadAuthorized && object.UnloadTransactionID != 0, nil
}

// authorizeUnload binds the exact object to its preauthorized cleanup
// transaction; LOAD provenance cannot be reused as cleanup evidence.
func (r *brokerAuditResolver) authorizeUnload(cookie, transactionID uint64) error {
	if r == nil || cookie == 0 || transactionID == 0 {
		return errGuardProtocol
	}
	object, ok := r.objects[cookie]
	if !ok || object.UnloadAuthorized || object.UnloadTransactionID != 0 {
		return errGuardProtocol
	}
	object.UnloadAuthorized = true
	object.UnloadTransactionID = transactionID
	r.objects[cookie] = object
	return nil
}

func (r *brokerAuditResolver) observeRawSyscall(event guardRecord) error {
	if r == nil || event.Kind != 13 || event.TaskCookie == 0 || event.TaskStartNS == 0 || event.AuditSerial == 0 || event.AuditSerial > 0xffffffff {
		return errGuardProtocol
	}
	seconds := guardU64(event.Detail[:], 8)
	nanoseconds := guardU32(event.Detail[:], 16)
	if seconds > 0x7fffffffffffffff || nanoseconds >= 1000000000 {
		return errGuardProtocol
	}
	key := auditEventKey{seconds, nanoseconds / 1000000, event.AuditSerial}
	cmd := bpfCommand(uint64(event.Operation))
	identity := serialAuditIdentity{
		TaskCookie: event.TaskCookie,
		Command:    cmd,
		RawSyscall: true,
		Number:     uint64(event.Operation),
		Result:     int64(event.Result),
	}
	if old, exists := r.serials[key]; exists {
		if old.TaskCookie != identity.TaskCookie {
			return errGuardProtocol
		}
		identity.TransactionID = old.TransactionID
		if identity.Command == "" {
			identity.Command = old.Command
		}
	}
	r.serials[key] = identity
	return nil
}

func (r *brokerAuditResolver) resolveAuditBPF(event guardRecord) (auditBinding, error) {
	if r == nil || event.Kind != 8 || event.ObjectCookie == 0 || event.Operation > 1 {
		return auditBinding{}, errGuardProtocol
	}
	object, ok := r.objects[event.ObjectCookie]
	if !ok || object.ProgramID != event.KernelID || event.Operation == 0 && !object.LoadAuthorized || event.Operation == 1 && (!object.UnloadAuthorized || object.UnloadTransactionID == 0) {
		return auditBinding{}, errGuardProtocol
	}
	binding := auditBinding{TransactionID: object.LoadTransactionID}
	if event.Operation == 0 {
		binding.Command = "PROG_LOAD"
	} else {
		binding.TransactionID = object.UnloadTransactionID
	}
	if event.Flags&1 != 0 {
		if event.AuditSerial == 0 || event.TaskCookie == 0 {
			return auditBinding{}, errGuardProtocol
		}
		binding.TaskCookie = event.TaskCookie
		identity := serialAuditIdentity{TaskCookie: event.TaskCookie, TransactionID: binding.TransactionID, Command: binding.Command}
		key, err := event.auditKey()
		if err != nil {
			return auditBinding{}, err
		}
		if old, exists := r.serials[key]; exists {
			if old.TaskCookie != identity.TaskCookie {
				return auditBinding{}, errGuardProtocol
			}
			identity.RawSyscall = old.RawSyscall
			identity.Number = old.Number
			identity.Result = old.Result
		}
		r.serials[key] = identity
	}
	return binding, nil
}

func (r *brokerAuditResolver) resolveAuditSyscall(f auditFact) (auditBinding, error) {
	if r == nil || f.Serial == 0 || f.Kind != auditSyscallRecord {
		return auditBinding{}, errGuardProtocol
	}
	isBPF := f.Arch == auditArchX8664 && f.Syscall == 321 || f.Arch == auditArchI386 && f.Syscall == 357
	if isBPF && f.PID == r.brokerPID && len(r.transactions) > 0 {
		tx := r.transactions[0]
		if tx.Number != f.Argument0 || tx.Result != f.Result || tx.ExecutorCookie != r.brokerCookie || tx.Command != bpfCommand(f.Argument0) {
			return auditBinding{}, errGuardProtocol
		}
		r.transactions = r.transactions[1:]
		if observed, exists := r.serials[f.Key]; exists {
			if observed.TaskCookie != tx.ExecutorCookie || (observed.TransactionID != 0 && observed.TransactionID != tx.ID) || (observed.RawSyscall && (observed.Number != tx.Number || observed.Result != tx.Result)) {
				return auditBinding{}, errGuardProtocol
			}
		}
		delete(r.serials, f.Key)
		return auditBinding{TaskCookie: tx.ExecutorCookie, TransactionID: tx.ID, Command: tx.Command}, nil
	}
	if identity, ok := r.serials[f.Key]; ok {
		if isBPF {
			if !identity.RawSyscall || f.Arch != auditArchX8664 || f.Syscall != 321 || f.Argument0 != identity.Number || f.Result != identity.Result {
				return auditBinding{}, errGuardProtocol
			}
			delete(r.serials, f.Key)
			return auditBinding{TaskCookie: identity.TaskCookie, TransactionID: identity.TransactionID, Command: identity.Command}, nil
		}
		delete(r.serials, f.Key)
		return auditBinding{TaskCookie: identity.TaskCookie, TransactionID: identity.TransactionID}, nil
	}
	return auditBinding{}, fmt.Errorf("BPF_AUTH_UNAVAILABLE: audited actor has no proven task incarnation")
}
