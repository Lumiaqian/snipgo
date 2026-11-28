//go:build darwin

package agentipc

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func sameUser(c *net.UnixConn) bool {
	raw, err := c.SyscallConn()
	if err != nil {
		return false
	}
	allowed := false
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		allowed = err == nil && cred.Version == 0 && cred.Uid == uint32(os.Geteuid())
	})
	return err == nil && allowed
}
