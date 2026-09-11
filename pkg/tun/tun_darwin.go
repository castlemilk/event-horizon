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
	"strings"
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

// AddHostRoute points a single destination at this interface, leaving every
// other route — and therefore the host's own en0 traffic — alone.
func (t *Interface) AddHostRoute(dst string) error {
	log.Printf("[TUN] Routing %s via %s...", dst, t.Name)
	cmd := exec.Command("route", "-n", "add", "-host", dst, "-interface", t.Name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("route add -host %s -interface %s: %w: %s",
			dst, t.Name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// AddStarlinkRoute routes the dish's telemetry address through this interface.
func (t *Interface) AddStarlinkRoute() error { return t.AddHostRoute("192.168.100.1") }

func (t *Interface) Close() {
	if t.File != nil {
		t.File.Close()
	}
}

// Keep silence for unused unsafe
var _ = unsafe.Sizeof(0)
