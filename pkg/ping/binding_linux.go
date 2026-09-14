//go:build linux

package ping

import (
 "net"
 "golang.org/x/sys/unix"
)

func bindSocketToInterface(fd int, iface net.Interface) error {
 return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface.Name)
}

func pingArguments(b interfaceBinding, target string) []string {
 return []string{"-n", "-c", "2", "-i", "0.2", "-W", "1", "-w", "3", "-I", b.iface.Name, target}
}
