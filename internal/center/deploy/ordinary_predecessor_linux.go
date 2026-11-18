//go:build linux

package deploy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordauthority"
)

func ordinaryReadAuthorityComponent(root, name string, limit int64) ([]byte, error) {
	switch name {
	case "authority-key", "activation-ledger.jsonl", "activation-receipt.json":
	default:
		return nil, errOrdinaryUpgrade
	}
	dirs, err := ordinaryOpenPrivateDirectory(filepath.Join(root, "private"), composeLaunchPublisherUID)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	authority, private := dirs[len(dirs)-2], dirs[len(dirs)-1]
	if authority.before.Uid != composeLaunchPublisherUID || authority.before.Gid != composeLaunchPublisherUID || authority.before.Mode&0o7777 != 0o755 {
		return nil, errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(private.fd, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary authority component")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o600 || before.Uid != composeLaunchPublisherUID || before.Gid != composeLaunchPublisherUID || before.Nlink != 1 || before.Size <= 0 || before.Size > limit {
		return nil, errOrdinaryUpgrade
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(private.fd, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) || !ordinaryPrivateDirectoryUnchanged(dirs) {
		clear(body)
		return nil, errOrdinaryUpgrade
	}
	return body, nil
}

// Capture only hashes and authenticated public command bytes; no private key
// or database password is copied into the operation intent.
func ordinaryCapturePredecessorAuthority(root string) (recordauthority.VerifiedComposeState, ordinaryPredecessorIntent, error) {
	var predecessor ordinaryPredecessorIntent
	durable, err := recordauthority.LoadComposeState(root)
	if err != nil || recordauthority.VerifyActivationReceipt(root, durable) != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	state, err := recordauthority.PreviewComposeMembershipV2(root)
	if err != nil || state.MembershipVersion != 2 || state.MembershipRotation == nil || state.ActivationCommand != durable.ActivationCommand || state.DatabasePassword() != durable.DatabasePassword() {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	activation, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	rotation, err := state.MembershipRotation.MarshalBinary()
	if err != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	ledger, err := ordinaryReadAuthorityComponent(root, "activation-ledger.jsonl", 16<<10)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	keyBody, err := ordinaryReadAuthorityComponent(root, "authority-key", 1024)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	defer clear(keyBody)
	receipt, err := ordinaryReadAuthorityComponent(root, "activation-receipt.json", 16<<10)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	block, remainder := pem.Decode(keyBody)
	if block == nil || block.Type != "PRIVATE KEY" || len(remainder) != 0 {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return recordauthority.VerifiedComposeState{}, predecessor, errOrdinaryUpgrade
	}
	public := key.Public().(ed25519.PublicKey)
	predecessor = ordinaryPredecessorIntent{DeploymentID: string(state.DeploymentID), GenesisLedgerSHA256: ordinaryDigestHex(ledger), AuthorityPublicKeySHA256: ordinaryDigestHex(public), ActivationReceiptSHA256: ordinaryDigestHex(receipt)}
	again, err := recordauthority.PreviewComposeMembershipV2(root)
	if err != nil || again.MembershipRotation == nil || again.DatabasePassword() != state.DatabasePassword() {
		return recordauthority.VerifiedComposeState{}, ordinaryPredecessorIntent{}, errOrdinaryUpgrade
	}
	durableAgain, err := recordauthority.LoadComposeState(root)
	if err != nil || durableAgain.MembershipVersion != durable.MembershipVersion || durableAgain.ActivationCommand != durable.ActivationCommand || durableAgain.DatabasePassword() != durable.DatabasePassword() || recordauthority.VerifyActivationReceipt(root, durableAgain) != nil {
		return recordauthority.VerifiedComposeState{}, ordinaryPredecessorIntent{}, errOrdinaryUpgrade
	}
	againActivation, err1 := again.ActivationCommand.MarshalBinary()
	againRotation, err2 := again.MembershipRotation.MarshalBinary()
	if err1 != nil || err2 != nil || !bytes.Equal(activation, againActivation) || !bytes.Equal(rotation, againRotation) {
		return recordauthority.VerifiedComposeState{}, ordinaryPredecessorIntent{}, errOrdinaryUpgrade
	}
	return state, predecessor, nil
}
