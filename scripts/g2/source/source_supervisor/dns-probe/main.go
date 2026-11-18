// The pre-genesis diagnostic issues only a fixed empty-name-resolution probe.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type result struct {
	Version          int    `json:"version"`
	LabelSHA256      string `json:"label_sha256"`
	UpstreamRefused  bool   `json:"upstream_refused"`
	ResolverOutcome  string `json:"resolver_outcome"`
	ResolverRcode    int    `json:"resolver_rcode"`
}

func dnsPacket() ([]byte, string, error) {
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", err
	}
	label := "g2-" + hex.EncodeToString(random[2:])
	digest := sha256.Sum256([]byte(label))
	packet := make([]byte, 12, 64)
	copy(packet[:2], random[:2])
	binary.BigEndian.PutUint16(packet[2:4], 0x0100)
	binary.BigEndian.PutUint16(packet[4:6], 1)
	packet = append(packet, byte(len(label)))
	packet = append(packet, label...)
	packet = append(packet, 7, 'i', 'n', 'v', 'a', 'l', 'i', 'd', 0, 0, 1, 0, 1)
	return packet, hex.EncodeToString(digest[:]), nil
}

func query(address string, packet []byte) ([]byte, error) {
	connection, err := net.DialTimeout("udp4", address, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := connection.Write(packet); err != nil {
		return nil, err
	}
	buffer := make([]byte, 4096)
	count, err := connection.Read(buffer)
	if err != nil {
		return nil, err
	}
	return buffer[:count], nil
}

func run() error {
	if len(os.Args) != 1 || os.Getuid() != 10002 || os.Geteuid() != 10002 || os.Getgid() != 10002 || os.Getegid() != 10002 {
		return errors.New("diagnostic identity differs")
	}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGTERM)
	defer signal.Stop(stopped)
	packet, label, err := dnsPacket()
	if err != nil {
		return err
	}
	if _, err = query("127.0.0.1:53", packet); !errors.Is(err, syscall.ECONNREFUSED) {
		return errors.New("configured upstream was not unreachable")
	}
	response, err := query("127.0.0.11:53", packet)
	outcome, rcode := "noanswer", -1
	if err != nil {
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			return errors.New("embedded resolver did not answer or time out")
		}
		outcome = "timeout"
	} else {
		if len(response) < 12 || response[0] != packet[0] || response[1] != packet[1] ||
			response[2]&0x80 == 0 || binary.BigEndian.Uint16(response[6:8]) != 0 {
			return errors.New("embedded resolver response was not an empty DNS reply")
		}
		rcode = int(response[3] & 15)
	}
	body, err := json.Marshal(result{1, label, true, outcome, rcode})
	if err != nil {
		return err
	}
	file, err := os.OpenFile("/var/lib/postgresql/data/result.tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename("/var/lib/postgresql/data/result.tmp", "/var/lib/postgresql/data/result.json"); err != nil {
		return err
	}
	directory, err := os.Open("/var/lib/postgresql/data")
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	select {
	case <-stopped:
		os.Exit(143)
	case <-time.After(5 * time.Minute):
	}
	return errors.New("diagnostic was not joined")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "DNS diagnostic failed")
		os.Exit(1)
	}
}
