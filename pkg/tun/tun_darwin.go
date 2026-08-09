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

// ConfigureIP sets IP address and routing parameters on the virtual utun interface
func (t *Interface) ConfigureIP(ip, netmask, gateway string) error {
	log.Printf("[TUN] Configuring %s with IP %s, Gateway %s...", t.Name, ip, gateway)

	// ifconfig utunX ip gateway netmask netmask
	cmd := exec.Command("ifconfig", t.Name, ip, gateway, "netmask", netmask, "up")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[TUN] ifconfig warning: %s (%v)", string(output), err)
	} else {
		log.Printf("[TUN] Interface %s configured successfully: %s", t.Name, string(output))
	}

	return nil
}

// AddStarlinkRoute adds a static host route to Starlink Dish telemetry 192.168.100.1 via this interface
func (t *Interface) AddStarlinkRoute() error {
	log.Printf("[TUN] Adding static route for Starlink Dish (192.168.100.1) via %s...", t.Name)
	cmd := exec.Command("route", "-n", "add", "-host", "192.168.100.1", "-interface", t.Name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[TUN] Route command: %s (%v)", string(output), err)
	}
	return nil
}

func (t *Interface) Close() {
	if t.File != nil {
		t.File.Close()
	}
}

// Keep silence for unused unsafe
var _ = unsafe.Sizeof(0)
