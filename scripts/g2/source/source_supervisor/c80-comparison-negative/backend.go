package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"
)

type rootChannel struct {
	conn *net.UnixConn
	file *os.File
	peer unix.Ucred
}

func openRootChannel() (*rootChannel, error) {
	parent := os.Getppid()
	if parent <= 1 {
		return nil, errProtocol
	}
	typ, err := unix.GetsockoptInt(5, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || typ != unix.SOCK_SEQPACKET || unix.SetsockoptInt(5, unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
		return nil, errProtocol
	}
	peer, err := unix.GetsockoptUcred(5, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || int(peer.Pid) != parent || peer.Uid != 0 || peer.Gid != 0 || os.Getppid() != parent {
		return nil, errProtocol
	}
	unix.CloseOnExec(5)
	file := os.NewFile(5, "comparison-negative-root")
	if file == nil {
		return nil, errProtocol
	}
	converted, err := net.FileConn(file)
	if err != nil {
		_ = file.Close()
		return nil, errProtocol
	}
	connection, ok := converted.(*net.UnixConn)
	if !ok {
		_ = converted.Close()
		_ = file.Close()
		return nil, errProtocol
	}
	return &rootChannel{conn: connection, file: file, peer: *peer}, nil
}

// The root only acknowledges after independently observing this very backend.
// A fresh socketpair and credentials bind the reply to the direct root parent.
func (channel *rootChannel) ready(ctx context.Context, pid int32, started time.Time) error {
	deadline, ok := ctx.Deadline()
	if channel == nil || channel.conn == nil || !ok || ctx.Err() != nil || pid <= 0 || started.IsZero() ||
		channel.conn.SetDeadline(deadline) != nil || os.Getppid() != int(channel.peer.Pid) {
		return errProtocol
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = channel.conn.SetDeadline(time.Now())
		close(done)
	})
	defer func() {
		if !stop() {
			<-done
		}
		_ = channel.conn.SetDeadline(time.Time{})
	}()
	request := struct {
		Version      uint64 `json:"version"`
		Op           string `json:"op"`
		BackendPID   int32  `json:"backend_pid"`
		BackendStart string `json:"backend_start"`
		Role         string `json:"role"`
	}{1, "backend_ready", pid, started.UTC().Format(time.RFC3339Nano), runtimeRole}
	body, err := json.Marshal(request)
	if err != nil || len(body) == 0 || len(body) > frameLimit {
		return errProtocol
	}
	n, oobn, err := channel.conn.WriteMsgUnix(body, nil, nil)
	if err != nil || n != len(body) || oobn != 0 {
		return errProtocol
	}
	message := make([]byte, frameLimit+1)
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(4*256))
	n, oobn, flags, _, err := channel.conn.ReadMsgUnix(message, control)
	if err != nil {
		return errProtocol
	}
	controls, controlErr := unix.ParseSocketControlMessage(control[:oobn])
	bad := controlErr != nil
	credentials := 0
	for _, item := range controls {
		if item.Header.Level == unix.SOL_SOCKET && item.Header.Type == unix.SCM_RIGHTS {
			fds, _ := unix.ParseUnixRights(&item)
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
			bad = true
			continue
		}
		if item.Header.Level != unix.SOL_SOCKET || item.Header.Type != unix.SCM_CREDENTIALS {
			bad = true
			continue
		}
		credential, parseErr := unix.ParseUnixCredentials(&item)
		if parseErr != nil || credential == nil || *credential != channel.peer {
			bad = true
		}
		credentials++
	}
	if n == 0 || n > frameLimit || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || bad || credentials != 1 ||
		ctx.Err() != nil || os.Getppid() != int(channel.peer.Pid) {
		return errProtocol
	}
	fields, err := strictFields(message[:n], "version", "op")
	var version uint64
	var op string
	if err != nil || json.Unmarshal(fields["version"], &version) != nil ||
		json.Unmarshal(fields["op"], &op) != nil || version != 1 || op != "backend_ack" {
		return errProtocol
	}
	return nil
}

func openRuntimePool(ctx context.Context, ep endpoint, root *rootChannel) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connectionURL(ep))
	if err != nil || config.ConnConfig.Host != ep.host || config.ConnConfig.Port != ep.port ||
		config.ConnConfig.Database != applicationDB || config.ConnConfig.User != runtimeRole ||
		config.ConnConfig.Password != ep.password || config.ConnConfig.TLSConfig != nil || len(config.ConnConfig.Fallbacks) != 0 {
		return nil, errors.New("runtime configuration unavailable")
	}
	config.MaxConns = 1
	config.MinConns = 0
	var attempted atomic.Bool
	config.BeforeConnect = func(_ context.Context, _ *pgx.ConnConfig) error {
		if !attempted.CompareAndSwap(false, true) {
			return errors.New("runtime reconnect prohibited")
		}
		return nil
	}
	config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		var version, port int
		var pid int32
		var sessionRole, currentRole, database, host, name string
		var oid int64
		var superuser, login bool
		var started time.Time
		err := conn.QueryRow(ctx, `
			select current_setting('server_version_num')::integer, session_user, current_user,
			       r.oid::bigint, r.rolsuper, r.rolcanlogin, current_database(),
			       host(inet_server_addr()), inet_server_port(), current_setting('application_name'),
			       pg_catalog.pg_backend_pid(), a.backend_start
			from pg_catalog.pg_roles r
			join pg_catalog.pg_stat_activity a on a.pid=pg_catalog.pg_backend_pid()
			where r.rolname=current_user`).Scan(
			&version, &sessionRole, &currentRole, &oid, &superuser, &login, &database,
			&host, &port, &name, &pid, &started)
		if err != nil || version < 160000 || version >= 170000 || sessionRole != runtimeRole || currentRole != runtimeRole ||
			oid == 10 || superuser || !login || database != applicationDB || port != int(ep.port) ||
			!net.ParseIP(host).Equal(net.ParseIP(ep.host)) || name != "g2-c80-comparison-negative" ||
			pid <= 0 || started.IsZero() || root.ready(ctx, pid, started) != nil {
			return errors.New("runtime backend unavailable")
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("runtime pool unavailable")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("runtime backend unavailable")
	}
	return pool, nil
}
