package ping

import (
 "context"
 "fmt"
 "net"
 "syscall"
 "time"
)

type interfaceBinding struct { iface net.Interface; ip net.IP }

func resolveInterface(name string) (interfaceBinding, error) {
 if name == "" { return interfaceBinding{}, fmt.Errorf("select a network interface before testing") }
 iface, err := net.InterfaceByName(name)
 if err != nil { return interfaceBinding{}, fmt.Errorf("interface %s is unavailable or disconnected: %w", name, err) }
 addrs, err := iface.Addrs()
 if err != nil { return interfaceBinding{}, fmt.Errorf("read addresses for %s: %w", name, err) }
 return bindingFromAddresses(*iface, addrs)
}

func bindingFromAddresses(iface net.Interface, addrs []net.Addr) (interfaceBinding, error) {
 if iface.Flags&net.FlagUp == 0 { return interfaceBinding{}, fmt.Errorf("interface %s is down", iface.Name) }
 for _, addr := range addrs {
  ip, _, err := net.ParseCIDR(addr.String())
  if err == nil && ip.To4() != nil && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() {
   return interfaceBinding{iface: iface, ip: ip.To4()}, nil
  }
 }
 return interfaceBinding{}, fmt.Errorf("interface %s has no usable IPv4 address; connect it and wait for DHCP (IPv6-only tests are not supported)", iface.Name)
}

func (b interfaceBinding) control(network, address string, conn syscall.RawConn) error {
 var bindErr error
 if err := conn.Control(func(fd uintptr) { bindErr = bindSocketToInterface(int(fd), b.iface) }); err != nil { return err }
 return bindErr
}

// DNS and data sockets both use the selected interface. Binding only the source
// address does not constrain the outgoing interface on macOS.
func (b interfaceBinding) resolver(server string) *net.Resolver {
 return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
  d := net.Dialer{Timeout: 3 * time.Second, Control: b.control}
  if network == "tcp" { d.LocalAddr = &net.TCPAddr{IP: b.ip}; network = "tcp4" } else { d.LocalAddr = &net.UDPAddr{IP: b.ip}; network = "udp4" }
  return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
 }}
}

func (b interfaceBinding) dialContext(ctx context.Context, _, address string) (net.Conn, error) {
 d := net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: b.ip}, Control: b.control, Resolver: b.resolver("1.1.1.1")}
 return d.DialContext(ctx, "tcp4", address)
}
