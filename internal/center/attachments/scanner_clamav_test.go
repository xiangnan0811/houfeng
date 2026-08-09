package attachments

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fakeClamAVCommand = "zINSTREAM\x00"

func TestClamAVScannerStreamsExactINSTREAMProtocolOverTCPAndUnix(t *testing.T) {
	tests := []struct {
		name    string
		network string
	}{
		{name: "TCP", network: "tcp"},
		{name: "Unix", network: "unix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("abcdefghij")
			server := startFakeClamAVServer(t, tt.network, func(conn net.Conn) error {
				request, err := readFakeClamAVRequest(conn)
				if err != nil {
					return err
				}
				if request.command != fakeClamAVCommand {
					return fmt.Errorf("command = %q, want exact zINSTREAM NUL command", request.command)
				}
				wantChunks := [][]byte{[]byte("abcd"), []byte("efgh"), []byte("ij")}
				if !reflect.DeepEqual(request.chunks, wantChunks) {
					return fmt.Errorf("chunks = %q, want %q", request.chunks, wantChunks)
				}
				if !request.terminated {
					return errors.New("request did not include zero-length terminator")
				}
				if err := writeFakeClamAVReply(conn, []byte("stream: OK\x00")); err != nil {
					return err
				}
				return expectFakeClamAVClientClose(conn)
			})

			config := testClamAVScannerConfig(tt.network, server.address)
			config.ChunkSize = 4
			reader := &clamAVCountingReader{reader: bytes.NewReader(content)}
			scanner := mustNewTestClamAVScanner(t, config)
			code, err := scanner.Scan(context.Background(), reader)
			server.wait(t)

			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if code != ProcessorResultCodeClean {
				t.Fatalf("Scan() code = %q, want %q", code, ProcessorResultCodeClean)
			}
			if reader.bytesRead != int64(len(content)) {
				t.Fatalf("Scan() read %d bytes, want one-pass read of %d", reader.bytesRead, len(content))
			}
		})
	}
}

func TestClamAVScannerMapsExactRepliesToClosedResultCodes(t *testing.T) {
	tests := []struct {
		name      string
		reply     []byte
		wantCode  ProcessorResultCode
		wantError error
		forbidden string
		maxReply  int
	}{
		{
			name:     "clean",
			reply:    []byte("stream: OK\x00"),
			wantCode: ProcessorResultCodeClean,
			maxReply: len("stream: OK") + 1,
		},
		{
			name:      "malware found",
			reply:     []byte("stream: Eicar-Test-Signature FOUND\x00"),
			wantCode:  ProcessorResultCodeMalware,
			forbidden: "Eicar-Test-Signature",
			maxReply:  128,
		},
		{
			name:      "daemon error",
			reply:     []byte("INSTREAM classified-daemon-detail ERROR\x00"),
			wantCode:  ProcessorResultCodeScannerUnavailable,
			wantError: ErrClamAVScannerDaemon,
			forbidden: "classified-daemon-detail",
			maxReply:  128,
		},
		{
			name:      "non-exact clean",
			reply:     []byte("stream: OK\n\x00"),
			wantCode:  ProcessorResultCodeProcessingError,
			wantError: ErrClamAVScannerProtocol,
			forbidden: "stream: OK",
			maxReply:  128,
		},
		{
			name:      "found without signature",
			reply:     []byte("stream: FOUND\x00"),
			wantCode:  ProcessorResultCodeProcessingError,
			wantError: ErrClamAVScannerProtocol,
			forbidden: "stream: FOUND",
			maxReply:  128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
				request, err := readFakeClamAVRequest(conn)
				if err != nil {
					return err
				}
				if !request.terminated {
					return errors.New("request did not include zero-length terminator")
				}
				if err := writeFakeClamAVReply(conn, tt.reply); err != nil {
					return err
				}
				return expectFakeClamAVClientClose(conn)
			})

			config := testClamAVScannerConfig("tcp", server.address)
			config.MaxResponseBytes = tt.maxReply
			scanner := mustNewTestClamAVScanner(t, config)
			code, err := scanner.Scan(context.Background(), strings.NewReader("ordinary input"))
			server.wait(t)

			if code != tt.wantCode {
				t.Fatalf("Scan() code = %q, want %q", code, tt.wantCode)
			}
			if tt.wantError == nil {
				if err != nil {
					t.Fatalf("Scan() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("Scan() error = %v, want %v", err, tt.wantError)
			}
			assertClamAVErrorOmits(t, err, tt.forbidden)
		})
	}
}

func TestClamAVScannerRejectsTruncatedAndOversizedReplies(t *testing.T) {
	t.Run("truncated reply", func(t *testing.T) {
		server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
			if _, err := readFakeClamAVRequest(conn); err != nil {
				return err
			}
			if err := writeFakeClamAVReply(conn, []byte("stream: truncated-detail OK")); err != nil {
				return err
			}
			closeWriter, ok := conn.(interface{ CloseWrite() error })
			if !ok {
				return errors.New("fake TCP connection does not support CloseWrite")
			}
			if err := closeWriter.CloseWrite(); err != nil {
				return fmt.Errorf("half-close fake reply: %w", err)
			}
			return expectFakeClamAVClientClose(conn)
		})

		scanner := mustNewTestClamAVScanner(t, testClamAVScannerConfig("tcp", server.address))
		code, err := scanner.Scan(context.Background(), strings.NewReader("input"))
		server.wait(t)

		if code != ProcessorResultCodeProcessingError || !errors.Is(err, ErrClamAVScannerProtocol) {
			t.Fatalf("Scan() = (%q, %v), want processing_error and protocol error", code, err)
		}
		assertClamAVErrorOmits(t, err, "truncated-detail")
	})

	t.Run("reply exceeds configured maximum", func(t *testing.T) {
		const maxResponseBytes = 12
		server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
			if _, err := readFakeClamAVRequest(conn); err != nil {
				return err
			}
			if err := writeFakeClamAVReply(conn, bytes.Repeat([]byte("s"), maxResponseBytes+1)); err != nil {
				return err
			}
			return expectFakeClamAVClientClose(conn)
		})

		config := testClamAVScannerConfig("tcp", server.address)
		config.MaxResponseBytes = maxResponseBytes
		scanner := mustNewTestClamAVScanner(t, config)
		code, err := scanner.Scan(context.Background(), strings.NewReader("input"))
		server.wait(t)

		if code != ProcessorResultCodeProcessingError || !errors.Is(err, ErrClamAVScannerProtocol) {
			t.Fatalf("Scan() = (%q, %v), want processing_error and protocol error", code, err)
		}
		assertClamAVErrorOmits(t, err, strings.Repeat("s", maxResponseBytes+1))
	})
}

func TestClamAVScannerEnforcesPositiveInputLimitWithoutCleanVerdict(t *testing.T) {
	t.Run("exact limit terminates and can be clean", func(t *testing.T) {
		server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
			request, err := readFakeClamAVRequest(conn)
			if err != nil {
				return err
			}
			if got := fakeClamAVPayload(request); string(got) != "12345" {
				return fmt.Errorf("payload = %q, want exact bounded input", got)
			}
			if !request.terminated {
				return errors.New("exact-limit request was not terminated")
			}
			if err := writeFakeClamAVReply(conn, []byte("stream: OK\x00")); err != nil {
				return err
			}
			return expectFakeClamAVClientClose(conn)
		})

		config := testClamAVScannerConfig("tcp", server.address)
		config.MaxInputBytes = 5
		config.ChunkSize = 4
		scanner := mustNewTestClamAVScanner(t, config)
		code, err := scanner.Scan(context.Background(), strings.NewReader("12345"))
		server.wait(t)

		if err != nil || code != ProcessorResultCodeClean {
			t.Fatalf("Scan() = (%q, %v), want clean", code, err)
		}
	})

	t.Run("one byte over limit closes without terminator", func(t *testing.T) {
		const maxInputBytes = int64(5)
		content := []byte("classified-content-that-must-not-appear-in-errors")
		server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
			request, err := readFakeClamAVRequestUntilClose(conn)
			if err != nil {
				return err
			}
			if request.terminated {
				return errors.New("oversized request included a zero-length terminator")
			}
			for _, chunk := range request.chunks {
				if len(chunk) > 4 {
					return fmt.Errorf("chunk length = %d, exceeds configured chunk size", len(chunk))
				}
			}
			if got := int64(len(fakeClamAVPayload(request))); got > maxInputBytes {
				return fmt.Errorf("daemon received %d bytes, exceeds configured input maximum", got)
			}
			return nil
		})

		config := testClamAVScannerConfig("tcp", server.address)
		config.MaxInputBytes = maxInputBytes
		config.ChunkSize = 4
		reader := &clamAVCountingReader{reader: bytes.NewReader(content)}
		scanner := mustNewTestClamAVScanner(t, config)
		code, err := scanner.Scan(context.Background(), reader)
		server.wait(t)

		if code == ProcessorResultCodeClean || code != ProcessorResultCodeProcessingError ||
			!errors.Is(err, ErrClamAVScannerInputTooLarge) {
			t.Fatalf("Scan() = (%q, %v), want closed oversized-input result", code, err)
		}
		if reader.bytesRead != maxInputBytes+1 {
			t.Fatalf("Scan() read %d bytes, want bounded limit probe of %d", reader.bytesRead, maxInputBytes+1)
		}
		assertClamAVErrorOmits(t, err, string(content))
	})
}

func TestClamAVScannerHidesInputReadErrorsAndCloses(t *testing.T) {
	const sensitiveReadError = "classified filename and blob identity"
	server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
		request, err := readFakeClamAVRequestUntilClose(conn)
		if err != nil {
			return err
		}
		if request.terminated {
			return errors.New("failed input request included a zero-length terminator")
		}
		return nil
	})

	scanner := mustNewTestClamAVScanner(t, testClamAVScannerConfig("tcp", server.address))
	reader := &clamAVFailingReader{content: []byte("abc"), err: errors.New(sensitiveReadError)}
	code, err := scanner.Scan(context.Background(), reader)
	server.wait(t)

	if code != ProcessorResultCodeProcessingError || !errors.Is(err, ErrClamAVScannerInput) {
		t.Fatalf("Scan() = (%q, %v), want content-free input error", code, err)
	}
	assertClamAVErrorOmits(t, err, sensitiveReadError, "abc")
}

func TestClamAVScannerUnavailableEndpointIsStableAndOpaque(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "classified-clamd-endpoint.sock")
	config := testClamAVScannerConfig("unix", endpoint)
	scanner := mustNewTestClamAVScanner(t, config)

	code, err := scanner.Scan(context.Background(), strings.NewReader("classified input"))

	if code != ProcessorResultCodeScannerUnavailable || !errors.Is(err, ErrArchiveScannerUnavailable) {
		t.Fatalf("Scan() = (%q, %v), want scanner_unavailable", code, err)
	}
	assertClamAVErrorOmits(t, err, endpoint, "classified input")
}

func TestClamAVScannerCancellationIsStableOpaqueAndCloses(t *testing.T) {
	ready := make(chan struct{})
	server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
		if _, err := readFakeClamAVRequest(conn); err != nil {
			return err
		}
		close(ready)
		return expectFakeClamAVClientClose(conn)
	})

	config := testClamAVScannerConfig("tcp", server.address)
	config.OperationTimeout = time.Second
	scanner := mustNewTestClamAVScanner(t, config)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan clamAVScanResult, 1)
	go func() {
		code, err := scanner.Scan(ctx, strings.NewReader("classified cancel content"))
		result <- clamAVScanResult{code: code, err: err}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("fake daemon did not receive request")
	}
	cancel()

	var got clamAVScanResult
	select {
	case got = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("Scan() did not return after context cancellation")
	}
	server.wait(t)

	if got.code != ProcessorResultCodeProcessingError || !errors.Is(got.err, context.Canceled) {
		t.Fatalf("Scan() = (%q, %v), want processing_error and context cancellation", got.code, got.err)
	}
	assertClamAVErrorOmits(t, got.err, server.address, "classified cancel content")
}

func TestClamAVScannerDeadlineAndOperationTimeoutAreStableOpaqueAndClose(t *testing.T) {
	tests := []struct {
		name           string
		contextTimeout time.Duration
		operation      time.Duration
	}{
		{name: "operation timeout", operation: 100 * time.Millisecond},
		{name: "caller deadline", contextTimeout: 100 * time.Millisecond, operation: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := startFakeClamAVServer(t, "tcp", func(conn net.Conn) error {
				if _, err := readFakeClamAVRequest(conn); err != nil {
					return err
				}
				return expectFakeClamAVClientClose(conn)
			})

			config := testClamAVScannerConfig("tcp", server.address)
			config.OperationTimeout = tt.operation
			scanner := mustNewTestClamAVScanner(t, config)
			ctx := context.Background()
			cancel := func() {}
			if tt.contextTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.contextTimeout)
			}
			defer cancel()

			code, err := scanner.Scan(ctx, strings.NewReader("classified timeout content"))
			server.wait(t)

			if code != ProcessorResultCodeTimeout || !errors.Is(err, ErrClamAVScannerTimeout) {
				t.Fatalf("Scan() = (%q, %v), want timeout", code, err)
			}
			assertClamAVErrorOmits(t, err, server.address, "classified timeout content")
		})
	}
}

func TestClamAVScannerCanceledBeforeDialPreservesContextError(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "classified-canceled-endpoint.sock")
	scanner := mustNewTestClamAVScanner(t, testClamAVScannerConfig("unix", endpoint))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	code, err := scanner.Scan(ctx, strings.NewReader("classified input"))

	if code != ProcessorResultCodeProcessingError || !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() = (%q, %v), want processing_error and context cancellation", code, err)
	}
	assertClamAVErrorOmits(t, err, endpoint, "classified input")
}

func TestClamAVScannerValidatesFixedConfiguration(t *testing.T) {
	base := testClamAVScannerConfig("tcp", "127.0.0.1:3310")
	tests := []struct {
		name   string
		mutate func(*ClamAVScannerConfig)
	}{
		{name: "unknown network", mutate: func(config *ClamAVScannerConfig) { config.Network = "udp" }},
		{name: "empty address", mutate: func(config *ClamAVScannerConfig) { config.Address = "" }},
		{name: "malformed TCP address", mutate: func(config *ClamAVScannerConfig) { config.Address = "classified-address" }},
		{name: "NUL Unix address", mutate: func(config *ClamAVScannerConfig) { config.Network = "unix"; config.Address = "bad\x00path" }},
		{name: "zero dial timeout", mutate: func(config *ClamAVScannerConfig) { config.DialTimeout = 0 }},
		{name: "excessive dial timeout", mutate: func(config *ClamAVScannerConfig) { config.DialTimeout = time.Duration(1 << 62) }},
		{name: "zero operation timeout", mutate: func(config *ClamAVScannerConfig) { config.OperationTimeout = 0 }},
		{name: "excessive operation timeout", mutate: func(config *ClamAVScannerConfig) { config.OperationTimeout = time.Duration(1 << 62) }},
		{name: "zero chunk size", mutate: func(config *ClamAVScannerConfig) { config.ChunkSize = 0 }},
		{name: "excessive chunk size", mutate: func(config *ClamAVScannerConfig) { config.ChunkSize = math.MaxInt }},
		{name: "zero input maximum", mutate: func(config *ClamAVScannerConfig) { config.MaxInputBytes = 0 }},
		{name: "excessive input maximum", mutate: func(config *ClamAVScannerConfig) { config.MaxInputBytes = math.MaxInt64 }},
		{name: "zero response maximum", mutate: func(config *ClamAVScannerConfig) { config.MaxResponseBytes = 0 }},
		{name: "excessive response maximum", mutate: func(config *ClamAVScannerConfig) { config.MaxResponseBytes = math.MaxInt }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.mutate(&config)
			_, err := NewClamAVScanner(config)
			if !errors.Is(err, ErrInvalidClamAVScannerConfig) {
				t.Fatalf("NewClamAVScanner() error = %v, want invalid configuration", err)
			}
			assertClamAVErrorOmits(t, err, config.Address)
		})
	}
}

func TestClamAVScannerRealProbe(t *testing.T) {
	network := os.Getenv("HOUFENG_TEST_CLAMAV_NETWORK")
	address := os.Getenv("HOUFENG_TEST_CLAMAV_ADDRESS")
	if network == "" || address == "" {
		t.Skip("set HOUFENG_TEST_CLAMAV_NETWORK and HOUFENG_TEST_CLAMAV_ADDRESS to run the real ClamAV probe")
	}

	config := ClamAVScannerConfig{
		Network:          network,
		Address:          address,
		DialTimeout:      2 * time.Second,
		OperationTimeout: 30 * time.Second,
		ChunkSize:        32 * 1024,
		MaxInputBytes:    MiB,
		MaxResponseBytes: 4 * 1024,
	}
	scanner, err := NewClamAVScanner(config)
	if err != nil {
		t.Fatalf("NewClamAVScanner() error = %v", err)
	}
	code, err := scanner.Scan(context.Background(), strings.NewReader("houfeng explicit real ClamAV probe\n"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if code != ProcessorResultCodeClean {
		t.Fatalf("Scan() code = %q, want %q", code, ProcessorResultCodeClean)
	}
}

type fakeClamAVServer struct {
	address  string
	listener net.Listener
	done     chan error
}

func startFakeClamAVServer(t *testing.T, network string, handler func(net.Conn) error) *fakeClamAVServer {
	t.Helper()
	address := "127.0.0.1:0"
	if network == "unix" {
		address = filepath.Join(t.TempDir(), "clamd.sock")
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatalf("listen on fake ClamAV %s endpoint: %v", network, err)
	}
	server := &fakeClamAVServer{
		address:  listener.Addr().String(),
		listener: listener,
		done:     make(chan error, 1),
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			server.done <- acceptErr
			return
		}
		handlerErr := handler(conn)
		closeErr := conn.Close()
		if handlerErr == nil && closeErr != nil {
			handlerErr = closeErr
		}
		_ = listener.Close()
		server.done <- handlerErr
	}()
	return server
}

func (server *fakeClamAVServer) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-server.done:
		if err != nil {
			t.Fatalf("fake ClamAV server error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fake ClamAV server did not observe terminal connection close")
	}
}

type fakeClamAVRequest struct {
	command    string
	chunks     [][]byte
	terminated bool
}

func readFakeClamAVRequest(conn net.Conn) (fakeClamAVRequest, error) {
	request, err := readFakeClamAVRequestFrames(conn, false)
	if err != nil {
		return fakeClamAVRequest{}, err
	}
	if !request.terminated {
		return fakeClamAVRequest{}, errors.New("client closed before zero-length terminator")
	}
	return request, nil
}

func readFakeClamAVRequestUntilClose(conn net.Conn) (fakeClamAVRequest, error) {
	return readFakeClamAVRequestFrames(conn, true)
}

func readFakeClamAVRequestFrames(conn net.Conn, allowClose bool) (fakeClamAVRequest, error) {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fakeClamAVRequest{}, fmt.Errorf("set fake request deadline: %w", err)
	}
	command := make([]byte, len(fakeClamAVCommand))
	if _, err := io.ReadFull(conn, command); err != nil {
		return fakeClamAVRequest{}, fmt.Errorf("read ClamAV command: %w", err)
	}
	request := fakeClamAVRequest{command: string(command)}
	for {
		var header [4]byte
		_, err := io.ReadFull(conn, header[:])
		if err != nil {
			if allowClose && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				return request, nil
			}
			return fakeClamAVRequest{}, fmt.Errorf("read ClamAV frame header: %w", err)
		}
		length := binary.BigEndian.Uint32(header[:])
		if length == 0 {
			request.terminated = true
			return request, nil
		}
		if length > 1*1024*1024 {
			return fakeClamAVRequest{}, fmt.Errorf("unsafe fake frame length %d", length)
		}
		chunk := make([]byte, int(length))
		if _, err := io.ReadFull(conn, chunk); err != nil {
			return fakeClamAVRequest{}, fmt.Errorf("read ClamAV frame body: %w", err)
		}
		request.chunks = append(request.chunks, chunk)
	}
}

func fakeClamAVPayload(request fakeClamAVRequest) []byte {
	var payload []byte
	for _, chunk := range request.chunks {
		payload = append(payload, chunk...)
	}
	return payload
}

func writeFakeClamAVReply(conn net.Conn, reply []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("set fake reply deadline: %w", err)
	}
	for len(reply) > 0 {
		n, err := conn.Write(reply)
		if err != nil {
			return fmt.Errorf("write fake reply: %w", err)
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		reply = reply[n:]
	}
	return nil
}

func expectFakeClamAVClientClose(conn net.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("set fake close deadline: %w", err)
	}
	var extra [1]byte
	n, err := conn.Read(extra[:])
	if n != 0 {
		return fmt.Errorf("received %d unexpected bytes after terminal request", n)
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("wait for client close: %w", err)
	}
	return nil
}

func testClamAVScannerConfig(network, address string) ClamAVScannerConfig {
	return ClamAVScannerConfig{
		Network:          network,
		Address:          address,
		DialTimeout:      time.Second,
		OperationTimeout: time.Second,
		ChunkSize:        4,
		MaxInputBytes:    64,
		MaxResponseBytes: 128,
	}
}

func mustNewTestClamAVScanner(t *testing.T, config ClamAVScannerConfig) *ClamAVScanner {
	t.Helper()
	scanner, err := NewClamAVScanner(config)
	if err != nil {
		t.Fatalf("NewClamAVScanner() error = %v", err)
	}
	return scanner
}

func assertClamAVErrorOmits(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(err.Error(), value) {
			t.Fatalf("error %q exposes forbidden value %q", err, value)
		}
	}
}

type clamAVCountingReader struct {
	reader    *bytes.Reader
	bytesRead int64
}

func (reader *clamAVCountingReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	reader.bytesRead += int64(n)
	return n, err
}

type clamAVFailingReader struct {
	content []byte
	err     error
	done    bool
}

func (reader *clamAVFailingReader) Read(buffer []byte) (int, error) {
	if reader.done {
		return 0, reader.err
	}
	reader.done = true
	return copy(buffer, reader.content), reader.err
}

type clamAVScanResult struct {
	code ProcessorResultCode
	err  error
}
