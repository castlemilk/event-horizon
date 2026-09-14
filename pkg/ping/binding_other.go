//go:build !darwin && !linux

package ping

import (
 "fmt"
 "net"
)

func bindSocketToInterface(fd int, iface net.Interface) error { return fmt.Errorf("interface-bound diagnostics are unsupported on this platform") }
func pingArguments(b interfaceBinding, target string) []string { return nil }
