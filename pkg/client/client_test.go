package client_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/logging-file/internal"
	logclient "github.com/Muxcore-Media/logging-file/pkg/client"
)

const bufSize = 1 << 20

func startTestServer(t *testing.T, logPath string) (*grpc.ClientConn, func()) {
	t.Helper()
	mod := internal.NewModule(internal.Config{
		LogPath:    logPath,
		GRPCAddr:   "127.0.0.1:0",
		Level:      "debug",
		FlushMs:    1,
		SkipListen: true,
	})
	ctx := context.Background()
	if err := mod.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	lis := bufconn.Listen(bufSize)
	mod.SetListener(lis)
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	cleanup := func() {
		_ = mod.Stop(ctx)
		_ = conn.Close()
	}
	return conn, cleanup
}

func authedCtx(t *testing.T) context.Context {
	t.Helper()
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-caller-id", "test-module"))
}

func readOneLine(t *testing.T, path string) map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("expected log line")
	}
	var entry map[string]any
	if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return entry
}

func TestClientStructuredLogger(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "client.log")
	conn, cleanup := startTestServer(t, logPath)
	defer cleanup()

	c := logclient.New(conn, "caller-mod")
	var _ contracts.StructuredLogger = c
	var _ contracts.RedactingLogger = c

	c.Info(authedCtx(t), "from client", map[string]any{"key": "value"})

	entry := readOneLine(t, logPath)
	if entry["message"] != "from client" {
		t.Fatalf("message = %v", entry["message"])
	}
	if entry["source"] != "caller-mod" {
		t.Fatalf("source = %v", entry["source"])
	}
	if entry["key"] != "value" {
		t.Fatalf("key = %v", entry["key"])
	}
}

func TestClientDeniedWithoutAuth(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "deny.log")
	conn, cleanup := startTestServer(t, logPath)
	defer cleanup()

	c := logclient.New(conn, "caller-mod")
	c.Info(context.Background(), "should not land", nil)

	data, err := os.ReadFile(logPath)
	if err == nil && len(data) > 0 {
		t.Fatalf("expected empty log without auth, got %q", data)
	}
}
