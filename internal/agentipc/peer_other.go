//go:build !darwin

package agentipc

import "net"

func sameUser(*net.UnixConn) bool { return false }
