//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	pgSecretByteLen = 65
	fenceReceiptDir = "/var/lib/g2/control/maintenance/fences"
)

var fixedContainerPostgresExecutables = map[string]struct{ path, sha256 string }{
	"pg_dump":        {"/usr/lib/postgresql/16/bin/pg_dump", ""},
	"pg_restore":     {"/usr/lib/postgresql/16/bin/pg_restore", ""},
	"pg_controldata": {"/usr/lib/postgresql/16/bin/pg_controldata", ""},
}

type postgresServiceSecretRecord struct {
	bootID            string
	principalID       uint64
	daemonCookie      uint64
	service           string
	uid, gid          uint32
	pgdataDirFD       int
	pgdataStat        unix.Stat_t
	pgdataMountID     int
	secretStat        unix.Stat_t
	initGrantID       uint64
	initGeneration    uint64
	liveGrantID       uint64
	liveGeneration    uint64
	passwordSHA256    string
	initWait          *initContainerObservation
	initWaitCompleted bool
	handedOver        bool
}

type initContainerObservation struct {
	waitConn     net.Conn
	waitResponse *http.Response
	deadline     time.Time
}

type initSettlementResult struct {
	err error
}

func (o *initContainerObservation) close() {
	if o == nil {
		return
	}
	if o.waitResponse != nil && o.waitResponse.Body != nil {
		_ = o.waitResponse.Body.Close()
		o.waitResponse = nil
	}
	if o.waitConn != nil {
		_ = o.waitConn.Close()
		o.waitConn = nil
	}
}

func openInitContainerObservation(daemon *daemonRegistration, cid string, deadline time.Time) (_ *initContainerObservation, err error) {
	if !runtimeContainerID(cid) {
		return nil, errGuardProtocol
	}
	o := &initContainerObservation{deadline: deadline}
	defer func() {
		if err != nil {
			o.close()
		}
	}()
	o.waitConn, err = connectDocker(daemon, deadline)
	if err != nil {
		return nil, err
	}
	if err = writeDockerRequest(o.waitConn, "/v1.41/containers/"+cid+"/wait?condition=next-exit", false); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(o.waitConn, 8192)
	headers, err := boundedDockerHeaders(reader)
	if err != nil {
		return nil, err
	}
	o.waitResponse, err = http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(headers), reader)), &http.Request{Method: "POST"})
	if err != nil {
		return nil, err
	}
	if o.waitResponse.StatusCode != 200 {
		return nil, errGuardProtocol
	}
	return o, nil
}

func (o *initContainerObservation) finish(daemon *daemonRegistration, cid string, config map[string]any, actualStartObserved bool) error {
	if o == nil || !actualStartObserved || o.waitResponse == nil || time.Until(o.deadline) <= 0 {
		return errGuardProtocol
	}
	waitBody, err := io.ReadAll(io.LimitReader(o.waitResponse.Body, 8193))
	if err != nil || len(waitBody) > 8192 {
		return errGuardProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(waitBody))
	decoder.UseNumber()
	decoded, err := readJSONValue(decoder, 0)
	if err != nil {
		return err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return errGuardProtocol
	}
	result, ok := decoded.(map[string]any)
	if !ok || result["StatusCode"] != json.Number("0") {
		return errGuardProtocol
	}
	if errObj, exists := result["Error"]; exists && errObj != nil {
		errMap, ok := errObj.(map[string]any)
		if !ok || !exactKeys(errMap, "Message") || errMap["Message"] != "" {
			return errGuardProtocol
		}
	} else if !exactKeys(result, "StatusCode") && !exactKeys(result, "StatusCode", "Error") {
		return errGuardProtocol
	}
	item, err := recoveryDockerGET(daemon, "/v1.41/containers/"+cid+"/json", o.deadline)
	if err != nil || item["Id"] != cid {
		return errGuardProtocol
	}
	state, ok := item["State"].(map[string]any)
	if !ok || state["Running"] != false || state["ExitCode"] != json.Number("0") || state["Pid"] != json.Number("0") {
		return errGuardProtocol
	}
	_ = config
	return nil
}

func fdMountID(fd int) (int, error) {
	raw, err := readHeldProc(fmt.Sprintf("self/fdinfo/%d", fd), 65536)
	if err != nil {
		return -1, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != "mnt_id" {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || id < 0 {
			return -1, errGuardProtocol
		}
		return id, nil
	}
	return -1, errGuardProtocol
}

func openHeldPGDataDir(service string, uid, gid uint32) (int, unix.Stat_t, int, error) {
	var st unix.Stat_t
	switch service {
	case "app", "w1", "w2", "control":
	default:
		return -1, st, -1, errGuardProtocol
	}
	if uid == 0 || gid == 0 {
		return -1, st, -1, errGuardProtocol
	}
	parentPath := "/var/lib/g2/" + service
	parentFD, err := runtimeDirectory(parentPath, false)
	if err != nil {
		return -1, st, -1, err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat2(parentFD, "pgdata", &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, st, -1, errGuardProtocol
	}
	var named unix.Stat_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatat(parentFD, "pgdata", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(st, named) || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uid || st.Gid != gid || st.Mode&07777 != 0700 {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, -1, errGuardProtocol
	}
	mntID, err := fdMountID(fd)
	if err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, -1, err
	}
	return fd, st, mntID, nil
}

func recheckHeldPGDataDir(rec *postgresServiceSecretRecord) error {
	if rec == nil || rec.pgdataDirFD < 0 {
		return errGuardProtocol
	}
	fd, st, mntID, err := openHeldPGDataDir(rec.service, rec.uid, rec.gid)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var heldStat unix.Stat_t
	heldMnt, err := fdMountID(rec.pgdataDirFD)
	if err != nil || unix.Fstat(rec.pgdataDirFD, &heldStat) != nil {
		return errGuardProtocol
	}
	if heldStat.Dev != rec.pgdataStat.Dev || heldStat.Ino != rec.pgdataStat.Ino || heldStat.Uid != rec.uid || heldStat.Gid != rec.gid || heldStat.Mode != rec.pgdataStat.Mode || st.Dev != rec.pgdataStat.Dev || st.Ino != rec.pgdataStat.Ino || st.Uid != rec.uid || st.Gid != rec.gid || st.Mode != rec.pgdataStat.Mode || heldMnt != rec.pgdataMountID || mntID != rec.pgdataMountID {
		return errGuardProtocol
	}
	return nil
}

func readAndCommitInitSecret(service string, uid, gid uint32) (string, unix.Stat_t, error) {
	var st unix.Stat_t
	switch service {
	case "app", "w1", "w2", "control":
	default:
		return "", st, errGuardProtocol
	}
	if uid == 0 || gid == 0 {
		return "", st, errGuardProtocol
	}
	dirFD, err := runtimeDirectory("/run/g2", false)
	if err != nil {
		return "", st, errGuardProtocol
	}
	defer unix.Close(dirFD)
	leaf := "pg-secret-" + service
	fd, err := unix.Openat2(dirFD, leaf, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return "", st, errGuardProtocol
	}
	defer unix.Close(fd)
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || unix.Fstatat(dirFD, leaf, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(before, named) || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Uid != uid || before.Gid != gid || before.Mode&07777 != 0400 || before.Size != pgSecretByteLen {
		return "", st, errGuardProtocol
	}
	var buf [pgSecretByteLen + 1]byte
	defer clear(buf[:])
	n, err := unix.Pread(fd, buf[:], 0)
	if err != nil || n != pgSecretByteLen || buf[pgSecretByteLen-1] != '\n' {
		return "", st, errGuardProtocol
	}
	for i := range 64 {
		c := buf[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", st, errGuardProtocol
		}
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, leaf, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(before, after) || !sameTrustedStat(before, named) {
		return "", st, errGuardProtocol
	}
	sum := sha256.Sum256(buf[:64])
	return hex.EncodeToString(sum[:]), before, nil
}

func recheckInitSecretBeforeStart(service string, expected unix.Stat_t) error {
	dirFD, err := runtimeDirectory("/run/g2", false)
	if err != nil {
		return errGuardProtocol
	}
	defer unix.Close(dirFD)
	leaf := "pg-secret-" + service
	var named unix.Stat_t
	if unix.Fstatat(dirFD, leaf, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(expected, named) {
		return errGuardProtocol
	}
	return nil
}

func (rec *postgresServiceSecretRecord) close() {
	if rec == nil {
		return
	}
	rec.passwordSHA256 = ""
	if rec.initWait != nil {
		rec.initWait.close()
		rec.initWait = nil
	}
	if rec.pgdataDirFD >= 0 {
		_ = unix.Close(rec.pgdataDirFD)
		rec.pgdataDirFD = -1
	}
}

func (b *authorityBroker) clearServiceSecretForGrant(grant *brokerGrant) {
	if b == nil || grant == nil || b.serviceSecrets == nil {
		return
	}
	for name, rec := range b.serviceSecrets {
		if rec == nil {
			continue
		}
		if rec.initGrantID == grant.id && !rec.initWaitCompleted || rec.liveGrantID == grant.id {
			rec.close()
			delete(b.serviceSecrets, name)
		}
	}
}

type appEndpointFact struct {
	pid        uint32
	startTicks uint64
	netnsDev   uint64
	netnsIno   uint64
	networkID  string
	endpointID string
	ipv4       string
	prefix     uint64
	gateway    string
	ipamSubnet string
	recoveryID any
}

func (e *appEndpointFact) projection() any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"pid":         jsonU64(uint64(e.pid)),
		"start_ticks": jsonU64(e.startTicks),
		"netns_dev":   jsonU64(e.netnsDev),
		"netns_ino":   jsonU64(e.netnsIno),
		"network_id":  e.networkID,
		"endpoint_id": e.endpointID,
		"ipv4":        e.ipv4,
		"prefix":      jsonU64(e.prefix),
		"gateway":     e.gateway,
		"ipam_subnet": e.ipamSubnet,
		"recovery_id": e.recoveryID,
	}
}

func (e *appEndpointFact) same(other *appEndpointFact) bool {
	if e == nil || other == nil {
		return e == other
	}
	return *e == *other
}

func recoveryDockerGETList(daemon *daemonRegistration, path string, deadline time.Time) ([]any, error) {
	if !strings.HasPrefix(path, "/v1.41/") || strings.ContainsAny(path, "\r\n ") {
		return nil, errGuardProtocol
	}
	conn, err := connectDocker(daemon, deadline)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	message := []byte("GET " + path + " HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	for len(message) > 0 {
		n, e := conn.Write(message)
		if e != nil {
			return nil, e
		}
		if n <= 0 {
			return nil, io.ErrUnexpectedEOF
		}
		message = message[n:]
	}
	reader := bufio.NewReaderSize(conn, 8192)
	headers, err := boundedDockerHeaders(reader)
	if err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(headers), reader)), &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errGuardProtocol
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errGuardProtocol
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	value, err := readJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errGuardProtocol
	}
	result, ok := value.([]any)
	if !ok {
		return nil, errGuardProtocol
	}
	return result, nil
}

func validRecoveryID(rid string) bool {
	if len(rid) < 1 || len(rid) > 128 {
		return false
	}
	for _, c := range rid {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func readFenceNetworkReceipts(rid, appCID string) (string, error) {
	if !validRecoveryID(rid) || !runtimeContainerID(appCID) {
		return "", errGuardProtocol
	}
	parentFD, err := runtimeDirectory("/var/lib/g2/control/maintenance", false)
	if err != nil {
		return "", errGuardProtocol
	}
	defer unix.Close(parentFD)
	dirFD, err := unix.Openat2(parentFD, "fences", &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return "", errGuardProtocol
	}
	defer unix.Close(dirFD)
	var dirStat, namedDir unix.Stat_t
	if unix.Fstat(dirFD, &dirStat) != nil || unix.Fstatat(parentFD, "fences", &namedDir, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(dirStat, namedDir) || dirStat.Mode&unix.S_IFMT != unix.S_IFDIR || dirStat.Uid != 10002 || dirStat.Gid != 10002 || dirStat.Mode&07777 != 0700 {
		return "", errGuardProtocol
	}
	origRaw, origStat, err := readAtRegular(dirFD, rid+".json", maxControlFrame)
	if err != nil || origStat.Uid != 10002 || origStat.Gid != 10002 || origStat.Mode&07777 != 0600 || origStat.Nlink != 1 {
		return "", errGuardProtocol
	}
	netRaw, netStat, err := readAtRegular(dirFD, rid+".network.json", maxControlFrame)
	if err != nil || netStat.Uid != 10002 || netStat.Gid != 10002 || netStat.Mode&07777 != 0600 || netStat.Nlink != 1 {
		return "", errGuardProtocol
	}
	origDoc, err := recoveryDocument(origRaw, maxControlFrame)
	if err != nil || !exactKeys(origDoc, "version", "request", "containers") || origDoc["version"] != json.Number("1") {
		return "", errGuardProtocol
	}
	reqDoc, ok := origDoc["request"].(map[string]any)
	if !ok || !exactKeys(reqDoc, "RecoveryID", "Project", "WorkingDirectory", "DatabaseContainerID", "ControlConfigPath") || reqDoc["RecoveryID"] != rid || reqDoc["Project"] != "g2app" || reqDoc["WorkingDirectory"] != "/opt/g2/compose" || reqDoc["DatabaseContainerID"] != appCID || reqDoc["ControlConfigPath"] != "/var/lib/g2/control/maintenance/config.json" {
		return "", errGuardProtocol
	}
	ctrs, ok := origDoc["containers"].([]any)
	if !ok || len(ctrs) != 1 {
		return "", errGuardProtocol
	}
	ctrObj, ok := ctrs[0].(map[string]any)
	if !ok || ctrObj["id"] != appCID {
		return "", errGuardProtocol
	}
	netDoc, err := recoveryDocument(netRaw, maxControlFrame)
	if err != nil || !exactKeys(netDoc, "version", "recovery_id", "network_id") || netDoc["version"] != json.Number("1") || netDoc["recovery_id"] != rid {
		return "", errGuardProtocol
	}
	netID, ok := netDoc["network_id"].(string)
	if !ok || !runtimeContainerID(netID) {
		return "", errGuardProtocol
	}
	return netID, nil
}

func ipv4ToHexLE(addr netip.Addr) string {
	b := addr.As4()
	return fmt.Sprintf("%02X%02X%02X%02X", b[3], b[2], b[1], b[0])
}

func verifyContainerKernelNetworkState(pid uint32, ipv4Str, gatewayStr, subnetStr string) error {
	addr, err := netip.ParseAddr(ipv4Str)
	if err != nil || !addr.Is4() {
		return errGuardProtocol
	}
	gw, err := netip.ParseAddr(gatewayStr)
	if err != nil || !gw.Is4() {
		return errGuardProtocol
	}
	prefix, err := netip.ParsePrefix(subnetStr)
	if err != nil || !prefix.Addr().Is4() || prefix.Masked() != prefix || !prefix.Contains(addr) || !prefix.Contains(gw) {
		return errGuardProtocol
	}

	// 1. /proc/<pid>/net/dev: exactly lo and one eth interface
	devRaw, err := readHeldProc(fmt.Sprintf("%d/net/dev", pid), 65536)
	if err != nil {
		return errGuardProtocol
	}
	devLines := strings.Split(strings.TrimSpace(string(devRaw)), "\n")
	if len(devLines) != 4 {
		return errGuardProtocol
	}
	ifaces := make(map[string]bool)
	var ethName string
	for _, line := range devLines[2:] {
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			return errGuardProtocol
		}
		name = strings.TrimSpace(name)
		if name == "" || ifaces[name] {
			return errGuardProtocol
		}
		ifaces[name] = true
		if name != "lo" {
			if !strings.HasPrefix(name, "eth") || ethName != "" {
				return errGuardProtocol
			}
			ethName = name
		}
	}
	if !ifaces["lo"] || ethName == "" {
		return errGuardProtocol
	}

	// 2. /proc/<pid>/net/if_inet6: must be empty (IPv6 disabled or no IPv6 addresses)
	inet6Raw, err := readHeldProc(fmt.Sprintf("%d/net/if_inet6", pid), 65536)
	if err != nil {
		if !errors.Is(err, unix.ENOENT) {
			return errGuardProtocol
		}
	} else if strings.TrimSpace(string(inet6Raw)) != "" {
		return errGuardProtocol
	}

	// 3. /proc/<pid>/net/route: only ethName routes (subnet link route and default gateway route)
	routeRaw, err := readHeldProc(fmt.Sprintf("%d/net/route", pid), 65536)
	if err != nil {
		return errGuardProtocol
	}
	routeLines := strings.Split(strings.TrimSpace(string(routeRaw)), "\n")
	if len(routeLines) != 3 {
		return errGuardProtocol
	}
	wantNetHex := ipv4ToHexLE(prefix.Addr())
	wantGwHex := ipv4ToHexLE(gw)
	maskBits := prefix.Bits()
	maskVal := uint32(0xffffffff) << (32 - maskBits)
	wantMaskHex := fmt.Sprintf("%02X%02X%02X%02X", byte(maskVal), byte(maskVal>>8), byte(maskVal>>16), byte(maskVal>>24))
	seenDefault, seenSubnet := false, false
	for _, line := range routeLines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] != ethName {
			return errGuardProtocol
		}
		dest, gateway, flags, mask := fields[1], fields[2], fields[3], fields[7]
		if dest == "00000000" && gateway == wantGwHex && flags == "0003" && mask == "00000000" {
			seenDefault = true
		} else if dest == wantNetHex && gateway == "00000000" && flags == "0001" && mask == wantMaskHex {
			seenSubnet = true
		} else {
			return errGuardProtocol
		}
	}
	if !seenDefault || !seenSubnet {
		return errGuardProtocol
	}

	// 4. /proc/<pid>/net/fib_trie: must contain 127.0.0.1 and ipv4Str, and no other 172.x host /32 LOCAL address
	trieRaw, err := readHeldProc(fmt.Sprintf("%d/net/fib_trie", pid), 262144)
	if err != nil {
		return errGuardProtocol
	}
	trieText := string(trieRaw)
	if !strings.Contains(trieText, ipv4Str) || !strings.Contains(trieText, "127.0.0.1") {
		return errGuardProtocol
	}
	bcast := netip.AddrFrom4([4]byte{
		prefix.Addr().As4()[0] | ^byte(maskVal>>24),
		prefix.Addr().As4()[1] | ^byte(maskVal>>16),
		prefix.Addr().As4()[2] | ^byte(maskVal>>8),
		prefix.Addr().As4()[3] | ^byte(maskVal),
	}).String()
	for _, line := range strings.Split(trieText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|-- ") {
			ipStr := strings.TrimPrefix(trimmed, "|-- ")
			if ipStr != "0.0.0.0" && ipStr != "127.0.0.0" && ipStr != "127.0.0.1" && ipStr != "127.255.255.255" && ipStr != prefix.Addr().String() && ipStr != ipv4Str && ipStr != bcast {
				return errGuardProtocol
			}
		}
	}

	// 5. /proc/<pid>/net/tcp and tcp6: verify 0.0.0.0:5432 listener owned by pid, 127.0.0.11 DNS listener, no IPv6 listener
	tcp6Raw, err := readHeldProc(fmt.Sprintf("%d/net/tcp6", pid), 65536)
	if err != nil {
		return errGuardProtocol
	}
	for _, line := range strings.Split(strings.TrimSpace(string(tcp6Raw)), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[3] == "0A" {
			return errGuardProtocol
		}
	}
	tcpRaw, err := readHeldProc(fmt.Sprintf("%d/net/tcp", pid), 65536)
	if err != nil {
		return errGuardProtocol
	}
	var pgInode uint64
	dnsTCPCount := 0
	for _, line := range strings.Split(strings.TrimSpace(string(tcpRaw)), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return errGuardProtocol
		}
		if fields[3] != "0A" {
			continue
		}
		inode, e := strconv.ParseUint(fields[9], 10, 64)
		if e != nil || inode == 0 {
			return errGuardProtocol
		}
		switch fields[1] {
		case "00000000:1538": // 0.0.0.0:5432
			if pgInode != 0 {
				return errGuardProtocol
			}
			pgInode = inode
		default:
			if strings.HasPrefix(fields[1], "0B00007F:") { // 127.0.0.11:<port>
				dnsTCPCount++
			} else {
				return errGuardProtocol
			}
		}
	}
	if pgInode == 0 || dnsTCPCount != 1 {
		return errGuardProtocol
	}
	fdEntries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil || len(fdEntries) == 0 || len(fdEntries) > 4096 {
		return errGuardProtocol
	}
	wantSocket := fmt.Sprintf("socket:[%d]", pgInode)
	owned := false
	for _, entry := range fdEntries {
		link, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, entry.Name()))
		if e == nil && link == wantSocket {
			owned = true
			break
		}
	}
	if !owned {
		return errGuardProtocol
	}
	return nil
}

func observeLiveAppEndpoint(daemon *daemonRegistration, grant *brokerGrant, admittedRID string, deadline time.Time) (*appEndpointFact, int, error) {
	if daemon == nil || grant == nil || grant.purpose != "postgres-live-app" || !runtimeContainerID(grant.containerID) {
		return nil, -1, errGuardProtocol
	}
	item, err := recoveryDockerGET(daemon, "/v1.41/containers/"+grant.containerID+"/json", deadline)
	if err != nil || item["Id"] != grant.containerID || item["Image"] != postgresImageID {
		return nil, -1, errGuardProtocol
	}
	state, ok := item["State"].(map[string]any)
	if !ok || state["Running"] != true {
		return nil, -1, errGuardProtocol
	}
	pidNum, ok := exactNumber(state["Pid"])
	if !ok || pidNum <= 1 || pidNum > math.MaxInt32 {
		return nil, -1, errGuardProtocol
	}
	pid := uint32(pidNum)
	pidfd, err := unix.PidfdOpen(int(pid), 0)
	if err != nil {
		return nil, -1, errGuardProtocol
	}
	keepPIDFD := false
	defer func() {
		if !keepPIDFD {
			_ = unix.Close(pidfd)
		}
	}()
	startTicks, err := procStartTicks(int(pid))
	if err != nil || startTicks == 0 || !pidfdSelects(pidfd, pid) {
		return nil, -1, errGuardProtocol
	}
	var nsStat, nsStatAfter unix.Stat_t
	if unix.Stat(fmt.Sprintf("/proc/%d/ns/net", pid), &nsStat) != nil || nsStat.Ino == 0 {
		return nil, -1, errGuardProtocol
	}
	netSettings, ok := item["NetworkSettings"].(map[string]any)
	if !ok {
		return nil, -1, errGuardProtocol
	}
	sandboxKey, _ := netSettings["SandboxKey"].(string)
	if sandboxKey != "" {
		var sbStat unix.Stat_t
		if unix.Stat(sandboxKey, &sbStat) != nil || sbStat.Dev != nsStat.Dev || sbStat.Ino != nsStat.Ino {
			return nil, -1, errGuardProtocol
		}
	}
	attached, ok := netSettings["Networks"].(map[string]any)
	if !ok || len(attached) != 1 {
		return nil, -1, errGuardProtocol
	}
	var netName string
	var epObj map[string]any
	for k, v := range attached {
		netName = k
		epObj, ok = v.(map[string]any)
		if !ok {
			return nil, -1, errGuardProtocol
		}
	}
	netID, _ := epObj["NetworkID"].(string)
	epID, _ := epObj["EndpointID"].(string)
	ipStr, _ := epObj["IPAddress"].(string)
	gwStr, _ := epObj["Gateway"].(string)
	prefixNum, prefixOK := exactNumber(epObj["IPPrefixLen"])
	if !runtimeContainerID(netID) || !runtimeContainerID(epID) || !prefixOK || prefixNum != 24 {
		return nil, -1, errGuardProtocol
	}
	netDoc, err := recoveryDockerGET(daemon, "/v1.41/networks/"+netID, deadline)
	if err != nil || netDoc["Id"] != netID || netDoc["Name"] != netName || netDoc["Driver"] != "bridge" || netDoc["Scope"] != "local" || netDoc["Internal"] != false {
		return nil, -1, errGuardProtocol
	}
	ipam, ok := netDoc["IPAM"].(map[string]any)
	if !ok || ipam["Driver"] != "default" {
		return nil, -1, errGuardProtocol
	}
	ipamConfigs, ok := ipam["Config"].([]any)
	if !ok || len(ipamConfigs) != 1 {
		return nil, -1, errGuardProtocol
	}
	ipamCfg, ok := ipamConfigs[0].(map[string]any)
	if !ok {
		return nil, -1, errGuardProtocol
	}
	subnetStr, _ := ipamCfg["Subnet"].(string)
	ipamGwStr, _ := ipamCfg["Gateway"].(string)
	if subnetStr == "" || ipamGwStr == "" || ipamGwStr != gwStr {
		return nil, -1, errGuardProtocol
	}
	var recoveryValue any
	if netName == "g2-initial-app" {
		if admittedRID != "" || subnetStr != "172.30.80.0/24" || gwStr != "172.30.80.1" || ipStr != "172.30.80.2" {
			return nil, -1, errGuardProtocol
		}
	} else if strings.HasPrefix(netName, "houfeng-source-") {
		rid := strings.TrimPrefix(netName, "houfeng-source-")
		if !validRecoveryID(rid) || (admittedRID != "" && rid != admittedRID) {
			return nil, -1, errGuardProtocol
		}
		labels, ok := netDoc["Labels"].(map[string]any)
		if !ok || !exactKeys(labels, "houfeng.source.recovery_id", "com.docker.compose.project") || labels["houfeng.source.recovery_id"] != rid || labels["com.docker.compose.project"] != "g2app" {
			return nil, -1, errGuardProtocol
		}
		pfx, e := netip.ParsePrefix(subnetStr)
		pool, _ := netip.ParsePrefix("172.29.0.0/16")
		if e != nil || pfx.Bits() != 24 || pfx.Masked() != pfx || !pool.Contains(pfx.Addr()) {
			return nil, -1, errGuardProtocol
		}
		receiptNetID, e := readFenceNetworkReceipts(rid, grant.containerID)
		if e != nil || receiptNetID != netID {
			return nil, -1, errGuardProtocol
		}
		recoveryValue = rid
	} else {
		return nil, -1, errGuardProtocol
	}
	containersMap, ok := netDoc["Containers"].(map[string]any)
	if !ok || len(containersMap) != 1 {
		return nil, -1, errGuardProtocol
	}
	memberObj, ok := containersMap[grant.containerID].(map[string]any)
	if !ok || memberObj["EndpointID"] != epID || memberObj["IPv4Address"] != ipStr+"/24" {
		return nil, -1, errGuardProtocol
	}
	if err := verifyContainerKernelNetworkState(pid, ipStr, gwStr, subnetStr); err != nil {
		return nil, -1, err
	}
	if unix.Stat(fmt.Sprintf("/proc/%d/ns/net", pid), &nsStatAfter) != nil || nsStatAfter.Dev != nsStat.Dev || nsStatAfter.Ino != nsStat.Ino || !pidfdSelects(pidfd, pid) {
		return nil, -1, errGuardProtocol
	}
	startAfter, err := procStartTicks(int(pid))
	if err != nil || startAfter != startTicks {
		return nil, -1, errGuardProtocol
	}
	keepPIDFD = true
	return &appEndpointFact{
		pid:        pid,
		startTicks: startTicks,
		netnsDev:   uint64(nsStat.Dev),
		netnsIno:   nsStat.Ino,
		networkID:  netID,
		endpointID: epID,
		ipv4:       ipStr,
		prefix:     prefixNum,
		gateway:    gwStr,
		ipamSubnet: subnetStr,
		recoveryID: recoveryValue,
	}, pidfd, nil
}

func expectedExecArgs(observation string, appIP string) ([]string, bool, error) {
	switch observation {
	case "negative_pg_dump":
		if appIP == "" {
			return nil, false, errGuardProtocol
		}
		return []string{
			"/usr/lib/postgresql/16/bin/pg_dump",
			"--host=" + appIP,
			"--port=5432",
			"--username=postgres",
			"--format=custom",
			"--no-owner",
			"--no-privileges",
			"houfeng",
		}, true, nil
	case "app_rollback_dump":
		if appIP == "" {
			return nil, false, errGuardProtocol
		}
		return []string{
			"/usr/lib/postgresql/16/bin/pg_dump",
			"--host=" + appIP,
			"--port=5432",
			"--username=postgres",
			"--format=custom",
			"houfeng",
		}, true, nil
	case "app_rollback_restore":
		if appIP == "" {
			return nil, false, errGuardProtocol
		}
		return []string{
			"/usr/lib/postgresql/16/bin/pg_restore",
			"--host=" + appIP,
			"--port=5432",
			"--username=postgres",
			"--dbname=houfeng",
			"--clean",
			"--if-exists",
			"--exit-on-error",
			"--single-transaction",
			"/var/lib/postgresql/data/g2-app-rollback.fifo",
		}, true, nil
	case "pg_controldata":
		return []string{
			"/usr/lib/postgresql/16/bin/pg_controldata",
			"/var/lib/postgresql/data",
		}, false, nil
	default:
		return nil, false, errGuardProtocol
	}
}

func verifyRollbackRestoreFIFO(rec *postgresServiceSecretRecord) error {
	if rec == nil || rec.pgdataDirFD < 0 {
		return errGuardProtocol
	}
	if err := recheckHeldPGDataDir(rec); err != nil {
		return err
	}
	var st unix.Stat_t
	if unix.Fstatat(rec.pgdataDirFD, "g2-app-rollback.fifo", &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return errGuardProtocol
	}
	if st.Mode&unix.S_IFMT != unix.S_IFIFO || st.Dev != rec.pgdataStat.Dev || st.Uid != rec.uid || st.Gid != rec.gid || st.Mode&07777 != 0600 {
		return errGuardProtocol
	}
	return nil
}

func openContainerExecBinary(rootfs, binPath string) (*os.File, executableKey, string, error) {
	var key executableKey
	if !runtimePath(rootfs) || !strings.HasPrefix(binPath, "/usr/lib/postgresql/16/bin/") {
		return nil, key, "", errGuardProtocol
	}
	fullPath := filepath.Join(rootfs, strings.TrimPrefix(binPath, "/"))
	file, digest, err := openTrustedRegular(fullPath, 128<<20)
	if err != nil {
		return nil, key, "", errGuardProtocol
	}
	var st unix.Stat_t
	if unix.Fstat(int(file.Fd()), &st) != nil || st.Mode&0111 == 0 {
		_ = file.Close()
		return nil, key, "", errGuardProtocol
	}
	dev, err := kernelDevice(uint64(st.Dev))
	if err != nil || st.Ino == 0 {
		_ = file.Close()
		return nil, key, "", errGuardProtocol
	}
	return file, executableKey{dev: dev, ino: st.Ino}, digest, nil
}

// verifyRuntimeExec validates the held process.json file against the complete
// source-derived OCI exec process policy, independent private password
// commitment and current APP endpoint tuple.
func verifyRuntimeExec(b *authorityBroker, grant *brokerGrant, path string, processFD int, digest string) error {
	if grant == nil || grant.facts == nil || !grant.facts.runtimeReady || grant.exec == nil || grant.exec.state != "admitted" || grant.exec.consumed || grant.exec.settled || processFD < 0 || !runtimePath(path) || filepath.Dir(path) != "/tmp" {
		return errGuardProtocol
	}
	if _, ok := digestString(digest); !ok {
		return errGuardProtocol
	}
	execAdm := grant.exec
	if execAdm.dockerExecID == "" {
		return errGuardProtocol
	}
	if b != nil {
		now, err := monoNowNS()
		if err != nil || now >= execAdm.deadlineNS || b.pendingEndpoint != nil || b.endpointClosed || execAdm.bootID != b.bootID || execAdm.grantGen != grant.generation || execAdm.endpointGen != b.endpointGeneration {
			return errGuardProtocol
		}
	}
	if b != nil {
		if err := checkDaemonConfigAbsent(nil); err != nil {
			return err
		}
	}
	tmpDir, err := runtimeDirectory("/tmp", true)
	if err != nil {
		return errGuardProtocol
	}
	defer unix.Close(tmpDir)
	var heldProcStat unix.Stat_t
	if unix.Fstat(processFD, &heldProcStat) != nil || heldProcStat.Mode&07777 != 0600 {
		return errGuardProtocol
	}
	dup, err := unix.FcntlInt(uintptr(processFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return errGuardProtocol
	}
	procFile := os.NewFile(uintptr(dup), filepath.Base(path))
	if procFile == nil {
		_ = unix.Close(dup)
		return errGuardProtocol
	}
	defer procFile.Close()
	if err := runtimeRecheckInput(tmpDir, "/tmp", filepath.Base(path), procFile, digest, heldProcStat, true); err != nil {
		return errGuardProtocol
	}
	raw, err := readHeldRegular(processFD, runtimeInputLimit)
	if err != nil {
		return errGuardProtocol
	}
	defer clear(raw)
	if hashBytes(raw) != digest {
		return errGuardProtocol
	}
	actual, err := runtimeJSON(raw)
	if err != nil {
		return errGuardProtocol
	}
	defer func() {
		if envSlice, ok := actual["env"].([]any); ok {
			for i := range envSlice {
				envSlice[i] = nil
			}
		}
		clear(actual)
	}()
	if err := verifyMountAndRootfsIdentities(grant.facts, nil); err != nil {
		return errGuardProtocol
	}
	if err := verifyRuntimeUserFiles(grant.facts); err != nil {
		return errGuardProtocol
	}
	initialSpec, err := buildExpectedRuntimeSpecStruct(b, grant)
	if err != nil || initialSpec == nil || initialSpec.Process == nil {
		return errGuardProtocol
	}
	var appIP string
	if execAdm.observation != "pg_controldata" {
		if grant.purpose != "postgres-live-app" || execAdm.endpoint == nil || execAdm.appPIDFD < 0 || !pidfdSelects(execAdm.appPIDFD, execAdm.endpoint.pid) {
			return errGuardProtocol
		}
		if b != nil {
			ownerID := grant.owner
			if p := b.principals[grant.owner]; p != nil && p.Role == "REALM_ROOT" {
				ownerID = b.bootOwner
			}
			daemon := b.daemons[ownerID]
			if daemon == nil || daemon.cookie != execAdm.daemonCookie || !livePIDFD(daemon.pidfd) {
				return errGuardProtocol
			}
			deadline, e := guestDeadline(15 * time.Second)
			if e != nil {
				return errGuardProtocol
			}
			recheckedEP, recheckedFD, e := observeLiveAppEndpoint(daemon, grant, b.admittedRecoveryID, deadline)
			if e != nil {
				return errGuardProtocol
			}
			_ = unix.Close(recheckedFD)
			if !execAdm.endpoint.same(recheckedEP) {
				return errGuardProtocol
			}
		}
		appIP = execAdm.endpoint.ipv4
	}
	wantArgs, needsPassword, err := expectedExecArgs(execAdm.observation, appIP)
	if err != nil {
		return err
	}
	if execAdm.observation == "app_rollback_restore" {
		if execAdm.secret == nil || verifyRollbackRestoreFIFO(execAdm.secret) != nil {
			return errGuardProtocol
		}
	}
	actualEnvStrings, err := runtimeStrings(actual["env"])
	if err != nil {
		return errGuardProtocol
	}
	defer func() {
		for i := range actualEnvStrings {
			actualEnvStrings[i] = ""
		}
	}()
	baseEnv := createDaemonEnvironment(false, grant.facts.hostname, nil, grant.facts.configEnv)
	if needsPassword {
		if execAdm.secret == nil || !execAdm.secret.handedOver || execAdm.secret.liveGrantID != grant.id || execAdm.secret.liveGeneration != grant.generation || execAdm.secret.passwordSHA256 == "" {
			return errGuardProtocol
		}
		if recheckHeldPGDataDir(execAdm.secret) != nil {
			return errGuardProtocol
		}
		var foundPassword string
		countPassword := 0
		for _, item := range actualEnvStrings {
			if strings.HasPrefix(item, "PGPASSWORD=") {
				foundPassword = strings.TrimPrefix(item, "PGPASSWORD=")
				countPassword++
			}
		}
		if countPassword != 1 || len(foundPassword) != 64 {
			return errGuardProtocol
		}
		var pwBytes [64]byte
		defer clear(pwBytes[:])
		for i := range 64 {
			c := foundPassword[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return errGuardProtocol
			}
			pwBytes[i] = c
		}
		sum := sha256.Sum256(pwBytes[:])
		if hex.EncodeToString(sum[:]) != execAdm.secret.passwordSHA256 {
			return errGuardProtocol
		}
		expectedEnv := replaceOrAppendEnvValues(append([]string(nil), baseEnv...), []string{"PGPASSWORD=" + foundPassword})
		if !reflect.DeepEqual(actualEnvStrings, expectedEnv) {
			return errGuardProtocol
		}
	} else {
		if !reflect.DeepEqual(actualEnvStrings, baseEnv) {
			return errGuardProtocol
		}
	}

	// Reconstruct expected specs.Process via ContainerExecStart / execSetPlatformOpt
	p := *initialSpec.Process
	p.Args = wantArgs
	p.Env = actualEnvStrings
	p.Cwd = grant.facts.cwd
	if p.Cwd == "" {
		p.Cwd = "/"
	}
	p.Terminal = false
	p.ConsoleSize = nil
	resolvedUser, err := resolveContainerUser(grant.facts.user, grant.facts.passwdRaw, grant.facts.hasPasswd, grant.facts.groupRaw, grant.facts.hasGroup)
	if err != nil {
		return errGuardProtocol
	}
	resolvedUser.Umask = initialSpec.Process.User.Umask
	p.User = resolvedUser
	if grant.facts.sysInfo.appArmorSupported {
		p.ApparmorProfile = defaultAppArmorName
	} else {
		p.ApparmorProfile = ""
	}
	p.Rlimits = nil

	expectedProcRaw, err := json.Marshal(&p)
	if err != nil {
		return errGuardProtocol
	}
	defer clear(expectedProcRaw)
	expectedProcMap, err := runtimeJSON(expectedProcRaw)
	if err != nil {
		return errGuardProtocol
	}
	defer func() {
		if envSlice, ok := expectedProcMap["env"].([]any); ok {
			for i := range envSlice {
				envSlice[i] = nil
			}
		}
		clear(expectedProcMap)
	}()
	if !reflect.DeepEqual(actual, expectedProcMap) {
		return errGuardProtocol
	}
	if err := verifyMountAndRootfsIdentities(grant.facts, nil); err != nil {
		return errGuardProtocol
	}
	if err := runtimeRecheckInput(tmpDir, "/tmp", filepath.Base(path), procFile, digest, heldProcStat, true); err != nil {
		return errGuardProtocol
	}
	return nil
}
