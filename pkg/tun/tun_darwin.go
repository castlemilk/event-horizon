package tun

/*
#include <sys/socket.h>
#include <sys/sys_domain.h>
#include <sys/ioctl.h>
#include <sys/kern_control.h>
#include <net/if.h>
#include <net/if_utun.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>

// Create a macOS utun interface and return file descriptor
static int create_utun_interface(char *ifname_out, int ifname_max) {
	int fd = socket(PF_SYSTEM, SOCK_DGRAM, SYSPROTO_CONTROL);
	if (fd < 0) return -1;

	struct ctl_info info;
	memset(&info, 0, sizeof(info));
	strncpy(info.ctl_name, UTUN_CONTROL_NAME, sizeof(info.ctl_name));

	if (ioctl(fd, CTLIOCGINFO, &info) < 0) {
		close(fd);
		return -2;
	}

	struct sockaddr_ctl addr;
	memset(&addr, 0, sizeof(addr));
	addr.sc_len = sizeof(addr);
	addr.sc_family = AF_SYSTEM;
	addr.ss_sysaddr = AF_SYS_CONTROL;
	addr.sc_id = info.ctl_id;
	addr.sc_unit = 0; // Allocate dynamic utun index

	if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
		close(fd);
		return -3;
	}

	socklen_t len = ifname_max;
	if (getsockopt(fd, SYSPROTO_CONTROL, UTUN_OPT_IFNAME, ifname_out, &len) < 0) {
		close(fd);
		return -4;
	}

	return fd;
}
*/
import "C"
import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

type Interface struct {
	Name string
	Fd   int
	File *os.File
}

// NewUtun creates a virtual macOS network interface (e.g. utun8)
func NewUtun() (*Interface, error) {
	var ifname [32]C.char
	fd := C.create_utun_interface(&ifname[0], C.int(len(ifname)))
	if fd < 0 {
		return nil, fmt.Errorf("failed to create macOS utun interface (error code %d)", fd)
	}

	name := C.GoString(&ifname[0])
	file := os.NewFile(uintptr(fd), name)

	log.Printf("[TUN] Created macOS virtual network interface: %s (fd: %d)", name, fd)

	return &Interface{
		Name: name,
		Fd:   int(fd),
		File: file,
	}, nil
}

// ConfigureIP sets the address and peer on the utun interface.
//
// Both this and AddHostRoute used to log a failure and return nil, so a bridge
// could report itself up while its interface had no address and no route. They
// now return the error with the command's own output attached — that text is
// usually the whole diagnosis ("File exists", "Permission denied").
func (t *Interface) ConfigureIP(ip, netmask, gateway string) error {
	log.Printf("[TUN] Configuring %s with IP %s, Gateway %s...", t.Name, ip, gateway)

	// ifconfig utunX <local> <peer> netmask <mask> up
	cmd := exec.Command("ifconfig", t.Name, ip, gateway, "netmask", netmask, "up")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ifconfig %s %s: %w: %s", t.Name, ip, err, strings.TrimSpace(string(output)))
	}
	log.Printf("[TUN] Interface %s configured: %s", t.Name, strings.TrimSpace(string(output)))
	return nil
}

// EnsureHostRoute points dst at this interface, replacing a STALE route
// when one is in the way.
//
// `route add` fails with "File exists" when a previous link died without
// cleaning up (unclean daemon death, killed CLI) — and the leftover points
// at a dead utun, blackholing the dish while everything reports healthy.
// Blindly deleting is worse: the route might belong to a LIVE link owned
// by another process. So replacement is conditional:
//
//  1. No existing route, or route already points here → add (or no-op).
//  2. Route points elsewhere and the interface is not a utun → refuse.
//     Only utun routes are ever ours to touch; en0 etc. are the host's.
//  3. Route points at another utun → check it is idle (packet counters
//     frozen over a short window). Idle means its owner is dead; delete
//     and add ours. Active means someone's link is alive; refuse rather
//     than steal it.
//
// runCmdFns are package vars so tests can stub the OS without root.
var runCmdFn = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// EnsureHostRoute implements the policy above. A nil idleWait disables
// the traffic check (tests); production passes ~3s.
func (t *Interface) EnsureHostRoute(dst string, idleWait time.Duration) error {
	cur, err := routeIfaceFor(dst)
	if err == nil && cur == t.Name {
		return nil // already ours
	}
	if err == nil && cur != "" && !isUtun(cur) {
		return fmt.Errorf("refusing to steal %s from non-utun interface %s", dst, cur)
	}
	if err == nil && cur != "" && isUtun(cur) && cur != t.Name {
		idle, cerr := utunIdle(cur, idleWait)
		if cerr != nil {
			return fmt.Errorf("cannot verify %s is idle, leaving route alone: %w", cur, cerr)
		}
		if !idle {
			return fmt.Errorf("route for %s belongs to live interface %s; refusing to steal it", dst, cur)
		}
		if out, derr := runCmdFn("route", "-n", "delete", "-host", dst); derr != nil {
			return fmt.Errorf("route delete -host %s: %w: %s", dst, derr, strings.TrimSpace(string(out)))
		}
		log.Printf("[TUN] removed stale route %s -> %s", dst, cur)
	}
	if out, aerr := runCmdFn("route", "-n", "add", "-host", dst, "-interface", t.Name); aerr != nil {
		return fmt.Errorf("route add -host %s -interface %s: %w: %s",
			dst, t.Name, aerr, strings.TrimSpace(string(out)))
	}
	return nil
}

// routeIfaceFor reports which interface the kernel would use for dst.
func routeIfaceFor(dst string) (string, error) {
	out, err := runCmdFn("route", "-n", "get", dst)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "interface:"); i >= 0 {
			return strings.TrimSpace(line[i+len("interface:"):]), nil
		}
	}
	return "", fmt.Errorf("no interface line in route get %s", dst)
}

func isUtun(iface string) bool { return strings.HasPrefix(iface, "utun") }

// utunIdle reports whether an interface's packet counters are frozen.
// Two samples of `netstat -ibn` idleWait apart; equal counters = idle.
// A zero wait skips the second sample and reports idle (tests only —
// production always waits, because a single sample proves nothing).
func utunIdle(iface string, idleWait time.Duration) (bool, error) {
	before, err := utunCounters(iface)
	if err != nil {
		return false, err
	}
	if idleWait <= 0 {
		return true, nil
	}
	time.Sleep(idleWait)
	after, err := utunCounters(iface)
	if err != nil {
		return false, err
	}
	return before == after, nil
}

type utunCount struct{ in, out uint64 }

func utunCounters(iface string) (utunCount, error) {
	out, err := runCmdFn("netstat", "-ibn")
	if err != nil {
		return utunCount{}, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != iface || len(f) < 7 {
			continue
		}
		// Right-anchored: the middle columns vary (a <Link#N> row has
		// 10 fields, an address row 12 with "-" placeholders), but the
		// trailing counters are stable: ...Ipkts Ierrs Ibytes Opkts
		// Oerrs Obytes Coll. A row with a placeholder in a counter slot
		// is skipped in favour of the interface's other row form.
		n := len(f)
		ipkts, err1 := strconv.ParseUint(f[n-6], 10, 64)
		ibytes, err2 := strconv.ParseUint(f[n-4], 10, 64)
		opkts, err3 := strconv.ParseUint(f[n-3], 10, 64)
		obytes, err4 := strconv.ParseUint(f[n-2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		return utunCount{ipkts + ibytes, opkts + obytes}, nil
	}
	return utunCount{}, fmt.Errorf("interface %s not in netstat output", iface)
}

// AddStarlinkRoute routes the dish's telemetry address through this interface.
func (t *Interface) AddStarlinkRoute() error { return t.EnsureHostRoute("192.168.100.1", 0) }

func (t *Interface) Close() {
	if t.File != nil {
		t.File.Close()
	}
}

// Keep silence for unused unsafe
var _ = unsafe.Sizeof(0)
