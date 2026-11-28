package agentipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSocketDisconnect(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sg-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	started, stopped := make(chan struct{}), make(chan struct{})
	s, err := Listen(dir, func(ctx context.Context, r Request) Response {
		close(started)
		<-ctx.Done()
		close(stopped)
		return Response{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.Join(dir, "agent.sock")
	for name, perm := range map[string]os.FileMode{dir: 0700, path: 0600} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != perm {
			t.Fatalf("permissions: %s %v", name, err)
		}
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintln(conn, `{"id":"test","method":"test"}`)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("same-user request not accepted")
	}
	conn.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("handler survived disconnect")
	}
}
