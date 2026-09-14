//go:build darwin

package ping

import (
 "net"
 "golang.org/x/sys/unix"
)

func bindSocketToInterface(fd int, iface net.Interface) error {
 return unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_BOUND_IF, iface.Index)
}

func pingArguments(b interfaceBinding, target string) []string {
 return []string{"-n", "-c", "2", "-i", "0.2", "-W", "1000", "-t", "3", "-b", b.iface.Name, "-S", b.ip.String(), target}
}
