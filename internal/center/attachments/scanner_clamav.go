package attachments

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidClamAVScannerConfig = errors.New("invalid clamav scanner configuration")
	ErrInvalidClamAVScan          = errors.New("invalid clamav scan")
	ErrClamAVScannerInput         = errors.New("clamav scanner input error")
	ErrClamAVScannerInputTooLarge = errors.New("clamav scanner input too large")
	ErrClamAVScannerProtocol      = errors.New("clamav scanner protocol error")
	ErrClamAVScannerDaemon        = errors.New("clamav scanner daemon error")
	ErrClamAVScannerTimeout       = errors.New("clamav scanner timeout")
)

const (
	clamAVINSTREAMCommand          = "zINSTREAM\x00"
	clamAVReplyPrefix              = "stream: "
	clamAVReplyINSTREAMErrorPrefix = "INSTREAM "
	clamAVReplyFoundSuffix         = " FOUND"
	clamAVReplyErrorSuffix         = " ERROR"
	clamAVMaximumAddressBytes      = 4 * 1024
	clamAVMaximumDialTimeout       = time.Minute
	clamAVMaximumOperationTimeout  = 10 * time.Minute
	clamAVMaximumChunkBytes        = 1 * 1024 * 1024
	clamAVMaximumResponseBytes     = 64 * 1024
	clamAVMinimumResponseBytes     = len("stream: OK") + 1
	clamAVReplyReadBufferBytes     = 512
)

var (
	errClamAVReplyTruncated = errors.New("truncated clamav reply")
	errClamAVReplyTooLarge  = errors.New("oversized clamav reply")
	errClamAVReplyTrailing  = errors.New("trailing clamav reply bytes")
)

type ClamAVScannerConfig struct {
	Network          string
	Address          string
	DialTimeout      time.Duration
	OperationTimeout time.Duration
	ChunkSize        int
	MaxInputBytes    int64
	MaxResponseBytes int
}

type ClamAVScanner struct {
	network          string
	address          string
	dialTimeout      time.Duration
	operationTimeout time.Duration
	chunkSize        int
	maxInputBytes    int64
	maxResponseBytes int
}

func NewClamAVScanner(config ClamAVScannerConfig) (*ClamAVScanner, error) {
	if !validClamAVScannerConfig(config) {
		return nil, ErrInvalidClamAVScannerConfig
	}
	return &ClamAVScanner{
		network:          config.Network,
		address:          config.Address,
		dialTimeout:      config.DialTimeout,
		operationTimeout: config.OperationTimeout,
		chunkSize:        config.ChunkSize,
		maxInputBytes:    config.MaxInputBytes,
		maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

func (scanner *ClamAVScanner) Scan(ctx context.Context, content io.Reader) (ProcessorResultCode, error) {
	if scanner == nil || ctx == nil || content == nil {
		return ProcessorResultCodeProcessingError, ErrInvalidClamAVScan
	}

	scanContext, cancel := context.WithTimeout(ctx, scanner.operationTimeout)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: scanner.dialTimeout}).DialContext(
		scanContext,
		scanner.network,
		scanner.address,
	)
	if err != nil {
		return clamAVIOFailure(scanContext, err)
	}
	defer conn.Close()

	deadline, ok := scanContext.Deadline()
	if !ok || conn.SetDeadline(deadline) != nil {
		return ProcessorResultCodeScannerUnavailable, ErrArchiveScannerUnavailable
	}
	stopContextClose := context.AfterFunc(scanContext, func() {
		_ = conn.Close()
	})
	defer stopContextClose()

	if err := writeClamAVBytes(conn, []byte(clamAVINSTREAMCommand)); err != nil {
		return clamAVIOFailure(scanContext, err)
	}
	if code, err := scanner.writeContent(scanContext, conn, content); err != nil {
		return code, err
	}
	if err := writeClamAVFrame(conn, nil); err != nil {
		return clamAVIOFailure(scanContext, err)
	}

	reply, err := readClamAVReply(conn, scanner.maxResponseBytes)
	if err != nil {
		switch {
		case errors.Is(err, errClamAVReplyTruncated),
			errors.Is(err, errClamAVReplyTooLarge),
			errors.Is(err, errClamAVReplyTrailing):
			return ProcessorResultCodeProcessingError, ErrClamAVScannerProtocol
		default:
			return clamAVIOFailure(scanContext, err)
		}
	}
	if err := scanContext.Err(); err != nil {
		return clamAVContextFailure(err)
	}
	return classifyClamAVReply(reply)
}

func (scanner *ClamAVScanner) writeContent(
	ctx context.Context,
	conn net.Conn,
	content io.Reader,
) (ProcessorResultCode, error) {
	buffer := make([]byte, scanner.chunkSize)
	remaining := scanner.maxInputBytes
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return clamAVContextFailure(err)
		}
		readBytes := int64(len(buffer))
		if readBytes > remaining {
			readBytes = remaining
		}
		n, readErr := io.ReadFull(content, buffer[:int(readBytes)])
		switch {
		case readErr == nil:
			if err := writeClamAVFrame(conn, buffer[:n]); err != nil {
				return clamAVIOFailure(ctx, err)
			}
			remaining -= int64(n)
		case errors.Is(readErr, io.EOF):
			return "", nil
		case errors.Is(readErr, io.ErrUnexpectedEOF):
			if err := writeClamAVFrame(conn, buffer[:n]); err != nil {
				return clamAVIOFailure(ctx, err)
			}
			return "", nil
		default:
			if err := ctx.Err(); err != nil {
				return clamAVContextFailure(err)
			}
			return ProcessorResultCodeProcessingError, ErrClamAVScannerInput
		}
	}

	var probe [1]byte
	n, readErr := io.ReadFull(content, probe[:])
	if n > 0 {
		return ProcessorResultCodeProcessingError, ErrClamAVScannerInputTooLarge
	}
	if errors.Is(readErr, io.EOF) {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return clamAVContextFailure(err)
	}
	return ProcessorResultCodeProcessingError, ErrClamAVScannerInput
}

func validClamAVScannerConfig(config ClamAVScannerConfig) bool {
	if len(config.Address) == 0 || len(config.Address) > clamAVMaximumAddressBytes ||
		strings.IndexByte(config.Address, 0) >= 0 ||
		config.DialTimeout <= 0 || config.DialTimeout > clamAVMaximumDialTimeout ||
		config.OperationTimeout <= 0 || config.OperationTimeout > clamAVMaximumOperationTimeout ||
		config.ChunkSize <= 0 || config.ChunkSize > clamAVMaximumChunkBytes ||
		config.MaxInputBytes <= 0 || config.MaxInputBytes > DefaultLimits().MaxFileBytes ||
		config.MaxResponseBytes < clamAVMinimumResponseBytes ||
		config.MaxResponseBytes > clamAVMaximumResponseBytes {
		return false
	}
	switch config.Network {
	case "tcp":
		host, port, err := net.SplitHostPort(config.Address)
		if err != nil || host == "" {
			return false
		}
		portNumber, err := strconv.Atoi(port)
		return err == nil && portNumber > 0 && portNumber <= 65535
	case "unix":
		return true
	default:
		return false
	}
}

func writeClamAVFrame(conn net.Conn, content []byte) error {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(content)))
	if err := writeClamAVBytes(conn, header[:]); err != nil {
		return err
	}
	return writeClamAVBytes(conn, content)
}

func writeClamAVBytes(conn net.Conn, content []byte) error {
	for len(content) > 0 {
		n, err := conn.Write(content)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		content = content[n:]
	}
	return nil
}

func readClamAVReply(conn net.Conn, maxBytes int) ([]byte, error) {
	readSize := clamAVReplyReadBufferBytes
	if maxBytes+1 < readSize {
		readSize = maxBytes + 1
	}
	buffer := make([]byte, readSize)
	reply := make([]byte, 0, maxBytes-1)
	totalBytes := 0
	for {
		n, err := conn.Read(buffer)
		for index, value := range buffer[:n] {
			totalBytes++
			if totalBytes > maxBytes {
				return nil, errClamAVReplyTooLarge
			}
			if value == 0 {
				if index != n-1 {
					return nil, errClamAVReplyTrailing
				}
				return reply, nil
			}
			reply = append(reply, value)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errClamAVReplyTruncated
			}
			return nil, err
		}
	}
}

func classifyClamAVReply(reply []byte) (ProcessorResultCode, error) {
	if bytes.Equal(reply, []byte("stream: OK")) {
		return ProcessorResultCodeClean, nil
	}
	if detail, ok := clamAVReplyDetail(reply, clamAVReplyFoundSuffix); ok && validClamAVReplyDetail(detail) {
		return ProcessorResultCodeMalware, nil
	}
	if detail, ok := clamAVDaemonErrorDetail(reply); ok && validClamAVReplyDetail(detail) {
		return ProcessorResultCodeScannerUnavailable, ErrClamAVScannerDaemon
	}
	return ProcessorResultCodeProcessingError, ErrClamAVScannerProtocol
}

func clamAVDaemonErrorDetail(reply []byte) ([]byte, bool) {
	if detail, ok := clamAVReplyDetail(reply, clamAVReplyErrorSuffix); ok {
		return detail, true
	}
	if !bytes.HasPrefix(reply, []byte(clamAVReplyINSTREAMErrorPrefix)) ||
		!bytes.HasSuffix(reply, []byte(clamAVReplyErrorSuffix)) {
		return nil, false
	}
	start := len(clamAVReplyINSTREAMErrorPrefix)
	end := len(reply) - len(clamAVReplyErrorSuffix)
	if start >= end {
		return nil, false
	}
	return reply[start:end], true
}

func clamAVReplyDetail(reply []byte, suffix string) ([]byte, bool) {
	if !bytes.HasPrefix(reply, []byte(clamAVReplyPrefix)) || !bytes.HasSuffix(reply, []byte(suffix)) {
		return nil, false
	}
	start := len(clamAVReplyPrefix)
	end := len(reply) - len(suffix)
	if start >= end {
		return nil, false
	}
	return reply[start:end], true
}

func validClamAVReplyDetail(detail []byte) bool {
	if len(detail) == 0 || detail[0] == ' ' || detail[len(detail)-1] == ' ' {
		return false
	}
	for _, value := range detail {
		if value < 0x20 || value == 0x7f {
			return false
		}
	}
	return true
}

func clamAVIOFailure(ctx context.Context, err error) (ProcessorResultCode, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return clamAVContextFailure(ctxErr)
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return ProcessorResultCodeTimeout, ErrClamAVScannerTimeout
	}
	return ProcessorResultCodeScannerUnavailable, ErrArchiveScannerUnavailable
}

func clamAVContextFailure(err error) (ProcessorResultCode, error) {
	if errors.Is(err, context.Canceled) {
		return ProcessorResultCodeProcessingError, context.Canceled
	}
	return ProcessorResultCodeTimeout, ErrClamAVScannerTimeout
}
