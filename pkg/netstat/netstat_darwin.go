package netstat

/*
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/sysctl.h>
#include <net/if.h>
#include <net/if_var.h>
#include <net/if_dl.h>
#include <net/route.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    char name[IFNAMSIZ];
    uint64_t bytes_in;
    uint64_t bytes_out;
    uint64_t packets_in;
    uint64_t packets_out;
    uint64_t errors_in;
    uint64_t errors_out;
    int is_up;
} if_stats_t;

static int get_interface_stats(if_stats_t *out_stats, int max_if) {
    int mib[6] = {CTL_NET, PF_ROUTE, 0, 0, NET_RT_IFLIST2, 0};
    size_t len = 0;
    if (sysctl(mib, 6, NULL, &len, NULL, 0) < 0) return -1;

    char *buf = (char *)malloc(len);
    if (!buf) return -1;
    if (sysctl(mib, 6, buf, &len, NULL, 0) < 0) {
        free(buf);
        return -1;
    }

    int count = 0;
    char *lim = buf + len;
    char *next = buf;

    while (next < lim && count < max_if) {
        struct if_msghdr *ifm = (struct if_msghdr *)next;
        next += ifm->ifm_msglen;

        if (ifm->ifm_type == RTM_IFINFO2) {
            struct if_msghdr2 *if2m = (struct if_msghdr2 *)ifm;
            struct sockaddr_dl *sdl = (struct sockaddr_dl *)(if2m + 1);

            if (sdl->sdl_nlen > 0 && sdl->sdl_nlen < IFNAMSIZ) {
                if_stats_t *stat = &out_stats[count];
                memset(stat, 0, sizeof(if_stats_t));
                strncpy(stat->name, sdl->sdl_data, sdl->sdl_nlen);
                stat->name[sdl->sdl_nlen] = '\0';

                stat->bytes_in = if2m->ifm_data.ifi_ibytes;
                stat->bytes_out = if2m->ifm_data.ifi_obytes;
                stat->packets_in = if2m->ifm_data.ifi_ipackets;
                stat->packets_out = if2m->ifm_data.ifi_opackets;
                stat->errors_in = if2m->ifm_data.ifi_ierrors;
                stat->errors_out = if2m->ifm_data.ifi_oerrors;
                stat->is_up = (if2m->ifm_flags & IFF_UP) ? 1 : 0;

                count++;
            }
        }
    }

    free(buf);
    return count;
}
*/
import "C"
import (
	"sync"
	"time"
)

type InterfaceStat struct {
	Name       string  `json:"name"`
	BytesIn    uint64  `json:"bytes_in"`
	BytesOut   uint64  `json:"bytes_out"`
	PacketsIn  uint64  `json:"packets_in"`
	PacketsOut uint64  `json:"packets_out"`
	ErrorsIn   uint64  `json:"errors_in"`
	ErrorsOut  uint64  `json:"errors_out"`
	IsUp       bool    `json:"is_up"`
	RxRateKBps float64 `json:"rx_rate_kbps"`
	TxRateKBps float64 `json:"tx_rate_kbps"`
}

type Monitor struct {
	mu        sync.Mutex
	prevStats map[string]InterfaceStat
	lastCheck time.Time
}

func NewMonitor() *Monitor {
	return &Monitor{
		prevStats: make(map[string]InterfaceStat),
		lastCheck: time.Now(),
	}
}

func (m *Monitor) GetInterfaceStats() []InterfaceStat {
	m.mu.Lock()
	defer m.mu.Unlock()

	var rawStats [64]C.if_stats_t
	count := C.get_interface_stats(&rawStats[0], 64)
	if count < 0 {
		return nil
	}

	now := time.Now()
	elapsedSec := now.Sub(m.lastCheck).Seconds()
	if elapsedSec <= 0 {
		elapsedSec = 1.0
	}

	var results []InterfaceStat

	for i := 0; i < int(count); i++ {
		st := rawStats[i]
		name := C.GoString(&st.name[0])

		if name == "" || name == "lo0" {
			continue
		}

		stat := InterfaceStat{
			Name:       name,
			BytesIn:    uint64(st.bytes_in),
			BytesOut:   uint64(st.bytes_out),
			PacketsIn:  uint64(st.packets_in),
			PacketsOut: uint64(st.packets_out),
			ErrorsIn:   uint64(st.errors_in),
			ErrorsOut:  uint64(st.errors_out),
			IsUp:       st.is_up != 0,
		}

		if prev, ok := m.prevStats[name]; ok && stat.BytesIn >= prev.BytesIn && stat.BytesOut >= prev.BytesOut {
			stat.RxRateKBps = float64(stat.BytesIn-prev.BytesIn) / 1024.0 / elapsedSec
			stat.TxRateKBps = float64(stat.BytesOut-prev.BytesOut) / 1024.0 / elapsedSec
		}

		m.prevStats[name] = stat
		results = append(results, stat)
	}

	m.lastCheck = now
	return results
}
