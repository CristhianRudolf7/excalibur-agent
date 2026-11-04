package daemon

import (
	"fmt"
	"net"
)

// SendNotify delivers message to socketPath over unixgram.
func SendNotify(socketPath, message string) error {
	if socketPath == "" {
		return fmt.Errorf("daemon: notify: no socket")
	}
	addr, err := net.ResolveUnixAddr("unixgram", socketPath)
	if err != nil {
		return err
	}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(message)); err != nil {
		return err
	}
	return nil
}
