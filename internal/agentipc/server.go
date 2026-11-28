// Package agentipc serves local opt-in desktop requests.
package agentipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Response struct {
	ID        string `json:"id"`
	OK        bool   `json:"ok"`
	Data      any    `json:"data,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
}

type Server struct {
	listener *net.UnixListener
	cancel   context.CancelFunc
}

func Listen(dir string, handle func(context.Context, Request) Response) (*Server, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("invalid socket directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "agent.sock")
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
		probe, err := net.DialTimeout("unix", path, 250*time.Millisecond)
		if err == nil {
			probe.Close()
			return nil, fmt.Errorf("desktop socket already in use")
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			current, err := os.Lstat(path)
			if err == nil && os.SameFile(info, current) {
				if err = os.Remove(path); err != nil {
					return nil, err
				}
			}
		}
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{listener: listener, cancel: cancel}
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			if !sameUser(conn) {
				conn.Close()
				continue
			}
			go serve(ctx, conn, handle)
		}
	}()
	return server, nil
}

func (s *Server) Close() error { s.cancel(); return s.listener.Close() }

func serve(parent context.Context, conn *net.UnixConn, handle func(context.Context, Request) Response) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	var message bytes.Buffer
	for {
		part, err := reader.ReadSlice('\n')
		if message.Len()+len(part) > 24<<20 {
			return
		}
		message.Write(part)
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return
		}
	}
	line := message.Bytes()
	var err error
	var req Request
	if err = json.Unmarshal(line, &req); err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})
	go func() { _, _ = reader.ReadByte(); cancel() }()
	reply := handle(ctx, req)
	reply.ID = req.ID
	if ctx.Err() == nil {
		_ = json.NewEncoder(conn).Encode(reply)
	}
}

func Call(ctx context.Context, path string, req Request, reply *Response) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	if err = json.NewDecoder(io.LimitReader(conn, 256<<20)).Decode(reply); err != nil {
		return err
	}
	if reply.ID != req.ID {
		return errors.New("response ID mismatch")
	}
	return nil
}
