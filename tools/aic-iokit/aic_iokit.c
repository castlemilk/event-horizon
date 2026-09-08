// aic_iokit.c — user-space LMAC bring-up for the AIC8800D80 over raw IOKit.
//
// The DriverKit driver (DriverKit/AIC8800D80/AIC8800D80Driver.cpp) proved the
// bring-up sequence but can only load with SIP disabled. This tool runs the
// SAME sequence entirely in user space via IOUSBLib — SIP stays on, no
// entitlements, no system extension. The one thing libusb could not do after
// MM_SET_STACK_START halts the bulk pipes was recover them: libusb exposes only
// one-ended libusb_clear_halt (ClearPipeStall). IOKit's IOUSBInterfaceInterface
// exposes AbortPipe + ClearPipeStallBothEnds + ResetPipe — the same IOUSBFamily
// operations DriverKit's IOUSBHostPipe::ClearStall uses. We call all three and
// log every return code so the recovery is observable.
//
// Build:  make          (or: clang -O2 -o aic_iokit aic_iokit.c \
//                              -framework IOKit -framework CoreFoundation)
// Run:    ./aic_iokit   (dongle must be in operational mode a69c:8d81/8d83)

#include <IOKit/IOKitLib.h>
#include <IOKit/IOCFPlugIn.h>
#include <IOKit/usb/IOUSBLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#define VID 0xa69c

// ---- LMAC protocol constants (from AIC8800D80Driver.cpp) --------------------
enum { TASK_MM = 0, TASK_SCANU = 4, TASK_ME = 5, TASK_SM = 6 };
enum {
    MM_RESET_REQ = 0x0000, MM_RESET_CFM = 0x0001,
    MM_START_REQ = 0x0002, MM_START_CFM = 0x0003,
    MM_ADD_IF_REQ = 0x0006, MM_ADD_IF_CFM = 0x0007,
    MM_SET_COEX_REQ = 0x0067,
    MM_SET_RF_CALIB_REQ = 0x006B, MM_SET_RF_CALIB_CFM = 0x006C,
    MM_SET_TXPWR_IDX_LVL_REQ = 0x0079,
    MM_SET_STACK_START_REQ = 0x007B, MM_SET_STACK_START_CFM = 0x007C,
    ME_CONFIG_REQ = 0x1400, ME_CONFIG_CFM = 0x1401,
    ME_CHAN_CONFIG_REQ = 0x1402, ME_CHAN_CONFIG_CFM = 0x1403,
    SCANU_START_REQ = 0x1000,
};

// ---- globals ---------------------------------------------------------------
static IOUSBInterfaceInterface500 **gIntf = NULL;
static UInt8 gIn[4], gInN = 0;    // bulk IN pipe refs
static UInt8 gOut[4], gOutN = 0;  // bulk OUT pipe refs
static uint16_t gSeq = 0;
static uint8_t txbuf[4096], rxbuf[4096];

#define LOG(...) do { printf(__VA_ARGS__); printf("\n"); fflush(stdout); } while (0)

// buildCmd writes the wrapped LMAC command into txbuf and returns its length.
//   [len:2 LE=paramLen+12][0x11][0x00][dummy u32=0]
//   [id:2][dest:2][src=seq:2][paramLen:2][param...]
static uint32_t buildCmd(uint16_t id, uint16_t dest, const uint8_t *param, uint16_t plen) {
    uint8_t *p = txbuf;
    uint16_t rec = (uint16_t)(plen + 12);
    p[0] = rec & 0xff; p[1] = (rec >> 8) & 0x0f;
    p[2] = 0x11; p[3] = 0x00;
    p[4] = p[5] = p[6] = p[7] = 0;
    p[8] = id & 0xff;   p[9]  = (id >> 8) & 0xff;
    p[10] = dest & 0xff; p[11] = (dest >> 8) & 0xff;
    // src_id must be DRV_TASK_ID (100) like the vendor + Go port, not a seq.
    (void)gSeq;
    p[12] = 100; p[13] = 0;
    p[14] = plen & 0xff; p[15] = (plen >> 8) & 0xff;
    for (uint16_t i = 0; i < plen; i++) p[16 + i] = param ? param[i] : 0;
    return (uint32_t)(16 + plen);
}

static int gOutSel = 0; // which OUT pipe currently works

static IOReturn sendCmd(uint16_t id, uint16_t dest, const uint8_t *param, uint16_t plen) {
    uint32_t total = buildCmd(id, dest, param, plen);
    // Try the selected OUT pipe; on failure, recover it and fall through to
    // the other OUT pipes (the firmware may switch command pipes after
    // stack_start).
    for (int attempt = 0; attempt < gOutN; attempt++) {
        int i = (gOutSel + attempt) % gOutN;
        IOReturn r = (*gIntf)->WritePipeTO(gIntf, gOut[i], txbuf, total, 500, 1000);
        if (r == kIOReturnSuccess) { gOutSel = i; return r; }
        LOG("  sendCmd id=0x%04x on OUT pipe %u -> 0x%08x; recovering pipe", id, gOut[i], r);
        (*gIntf)->AbortPipe(gIntf, gOut[i]);
        (*gIntf)->ClearPipeStallBothEnds(gIntf, gOut[i]);
        (*gIntf)->ResetPipe(gIntf, gOut[i]);
    }
    return kIOReturnTimeout;
}

// readAny reads one transfer from whichever IN pipe delivers data first.
static IOReturn readAny(uint32_t *outLen, int timeoutMs) {
    for (int i = 0; i < gInN; i++) {
        UInt32 size = sizeof(rxbuf);
        IOReturn r = (*gIntf)->ReadPipeTO(gIntf, gIn[i], rxbuf, &size, timeoutMs, timeoutMs);
        if (r == kIOReturnSuccess && size > 0) { *outLen = size; return kIOReturnSuccess; }
        if (r == kIOUSBPipeStalled) { (*gIntf)->ClearPipeStallBothEnds(gIntf, gIn[i]); }
    }
    return kIOReturnTimeout;
}

// waitCfm reads response frames until one in the requested task arrives.
static IOReturn waitCfm(uint16_t wantId, int tries) {
    uint16_t wantTask = wantId & 0xFC00;
    for (int i = 0; i < tries; i++) {
        uint32_t got = 0;
        if (readAny(&got, 300) != kIOReturnSuccess || got < 16) continue;
        if ((rxbuf[2] & 0x10) == 0) continue;            // data frame, not a cfm
        uint16_t id = (uint16_t)(rxbuf[4] | (rxbuf[5] << 8));
        if ((id & 0xFC00) == wantTask) return kIOReturnSuccess;
    }
    return kIOReturnTimeout;
}

// recoverPipes is the whole point: abort, clear-both-ends, and reset every bulk
// pipe after stack_start. Logs each return code so we can see what actually
// recovers the halted pipes where libusb's one-ended clear_halt failed.
static void recoverPipes(void) {
    for (int i = 0; i < gInN; i++) {
        IOReturn a = (*gIntf)->AbortPipe(gIntf, gIn[i]);
        IOReturn c = (*gIntf)->ClearPipeStallBothEnds(gIntf, gIn[i]);
        IOReturn s = (*gIntf)->ResetPipe(gIntf, gIn[i]);
        LOG("  recover IN  pipe %u: Abort=0x%08x ClearBothEnds=0x%08x ResetPipe=0x%08x", gIn[i], a, c, s);
    }
    for (int i = 0; i < gOutN; i++) {
        IOReturn a = (*gIntf)->AbortPipe(gIntf, gOut[i]);
        IOReturn c = (*gIntf)->ClearPipeStallBothEnds(gIntf, gOut[i]);
        IOReturn s = (*gIntf)->ResetPipe(gIntf, gOut[i]);
        LOG("  recover OUT pipe %u: Abort=0x%08x ClearBothEnds=0x%08x ResetPipe=0x%08x", gOut[i], a, c, s);
    }
}

static int gDoStack = 1;

static IOReturn runBringup(void) {
    static const uint8_t stackStart[4] = {0x01, 0x00, 0x20, 0x00};
    uint8_t zero112[112] = {0};
    static const uint8_t coex[16] = {1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};

    // 1. Start the MAC/PHY stack — this halts the bulk pipes. Skippable when
    // a previous session already started it (is_stack_start persists in fw).
    if (gDoStack) {
        LOG("stack_start (0x007B)...");
        sendCmd(MM_SET_STACK_START_REQ, TASK_MM, stackStart, 4);
        IOReturn cfm = waitCfm(MM_SET_STACK_START_CFM, 20);
        LOG("  stack_start cfm: %s", cfm == kIOReturnSuccess ? "received" : "none (expected — pipe halts)");
        usleep(1500 * 1000);
    } else {
        LOG("skipping stack_start (already started by a prior session)");
    }

    // *** the recovery libusb could not do ***
    LOG("recovering pipes (Abort + ClearPipeStallBothEnds + ResetPipe)...");
    recoverPipes();
    usleep(300 * 1000);

    // Drain the IN pipe: after stack_start the fw may have indications queued;
    // if the host never reads them the fw's USB task blocks on TX and stops
    // servicing OUT (which is exactly what a NAK-forever on OUT looks like).
    LOG("draining IN pipe(s)...");
    int drained = 0;
    for (int i = 0; i < 40; i++) {
        uint32_t got = 0;
        if (readAny(&got, 200) != kIOReturnSuccess) { if (i > 3) break; continue; }
        drained++;
        uint16_t id = got >= 6 ? (uint16_t)(rxbuf[4] | (rxbuf[5] << 8)) : 0;
        LOG("  IN frame %d: %u bytes type=0x%02x id=0x%04x  %02x %02x %02x %02x %02x %02x %02x %02x",
            drained, got, rxbuf[2], id, rxbuf[0], rxbuf[1], rxbuf[2], rxbuf[3], rxbuf[4], rxbuf[5], rxbuf[6], rxbuf[7]);
    }
    LOG("  drained %d frame(s)", drained);

    // 2. RF calibration.
    uint8_t txpwr[95] = {0};
    txpwr[0] = 1;
    for (int i = 1; i < 69; i++) txpwr[i] = 18;
    if (sendCmd(MM_SET_TXPWR_IDX_LVL_REQ, TASK_MM, txpwr, 95) == kIOReturnSuccess)
        waitCfm(MM_SET_TXPWR_IDX_LVL_REQ + 1, 10);
    uint8_t rfcal[24] = {0};
    rfcal[0] = 0x8f; rfcal[1] = 0x0f; rfcal[4] = 0x0f; rfcal[5] = 0x0f;
    rfcal[8] = 0x08; rfcal[9] = 0xc0; rfcal[10] = 0x34; rfcal[11] = 0x0c;
    rfcal[16] = 0x03; rfcal[17] = 0x42; rfcal[18] = 0x26;
    if (sendCmd(MM_SET_RF_CALIB_REQ, TASK_MM, rfcal, 24) == kIOReturnSuccess)
        waitCfm(MM_SET_RF_CALIB_CFM, 10);

    // 3. MAC init: reset -> me_config -> me_chan_config -> start -> coex -> add_if.
    sendCmd(MM_RESET_REQ, TASK_MM, NULL, 0);
    if (waitCfm(MM_RESET_CFM, 20) != kIOReturnSuccess) {
        LOG("reset: NO cfm after recovery — pipes did not survive stack_start");
        return kIOReturnError;
    }
    LOG("reset ok — PIPES SURVIVED stack_start (this is the win)");

    sendCmd(ME_CONFIG_REQ, TASK_ME, zero112, 112);
    waitCfm(ME_CONFIG_CFM, 10);

    uint8_t chanCfg[254] = {0};
    for (uint8_t ch = 1; ch <= 13; ch++) {
        uint16_t freq = 2407 + (uint16_t)ch * 5;
        int off = (ch - 1) * 6;
        chanCfg[off] = freq & 0xff; chanCfg[off + 1] = (freq >> 8) & 0xff;
        chanCfg[off + 4] = 20;
    }
    chanCfg[252] = 13;
    sendCmd(ME_CHAN_CONFIG_REQ, TASK_ME, chanCfg, 254);
    waitCfm(ME_CHAN_CONFIG_CFM, 10);

    uint8_t startReq[72] = {0};
    startReq[64] = 0x2c; startReq[65] = 0x01; startReq[68] = 20;
    sendCmd(MM_START_REQ, TASK_MM, startReq, 72);
    waitCfm(MM_START_CFM, 10);

    sendCmd(MM_SET_COEX_REQ, TASK_MM, coex, 16);
    waitCfm(MM_SET_COEX_REQ + 1, 10);

    uint8_t addIf[10] = {0};
    addIf[2] = 0x02; addIf[3] = 0x11; addIf[4] = 0x22; addIf[5] = 0x33; addIf[6] = 0x44; addIf[7] = 0x55;
    sendCmd(MM_ADD_IF_REQ, TASK_MM, addIf, 10);
    waitCfm(MM_ADD_IF_CFM, 10);
    LOG("station interface up — init complete");

    // 4. Scan and log beacons (proof the radio is alive).
    uint8_t scan[376] = {0};
    uint8_t chans[3] = {1, 6, 11};
    for (int i = 0; i < 3; i++) {
        uint16_t freq = 2407 + (uint16_t)chans[i] * 5;
        int off = i * 6;
        scan[off] = freq & 0xff; scan[off + 1] = (freq >> 8) & 0xff; scan[off + 4] = 20;
    }
    for (int i = 0; i < 6; i++) scan[352 + i] = 0xff;
    scan[367] = 3;
    LOG("scanning channels 1/6/11...");
    sendCmd(SCANU_START_REQ, TASK_SCANU, scan, 376);

    int beacons = 0;
    for (int i = 0; i < 60; i++) {
        uint32_t got = 0;
        if (readAny(&got, 300) != kIOReturnSuccess || got < 60 + 38) continue;
        if ((rxbuf[2] & 0x10) == 0) {
            uint8_t *m = rxbuf + 60;
            if (m[0] == 0x80 || m[0] == 0x50) {
                beacons++;
                LOG("  beacon: bssid %02x:%02x:%02x:%02x:%02x:%02x",
                    m[16], m[17], m[18], m[19], m[20], m[21]);
            }
        }
    }
    LOG("scan complete: %d beacons seen", beacons);
    return kIOReturnSuccess;
}

// ---- IOKit plumbing --------------------------------------------------------

static IOUSBDeviceInterface500 **openDevice(uint16_t *pidOut) {
    CFMutableDictionaryRef match = IOServiceMatching(kIOUSBDeviceClassName);

    io_iterator_t iter = 0;
    kern_return_t kr = IOServiceGetMatchingServices(kIOMainPortDefault, match, &iter);
    if (kr != KERN_SUCCESS) { LOG("IOServiceGetMatchingServices -> 0x%08x", kr); return NULL; }

    io_service_t svc;
    IOUSBDeviceInterface500 **dev = NULL;
    int seen = 0;
    while ((svc = IOIteratorNext(iter))) {
        seen++;
        IOCFPlugInInterface **plugin = NULL;
        SInt32 score = 0;
        IOUSBDeviceInterface500 **d = NULL;
        // Read VID/PID from the registry first so failures are attributable.
        UInt16 rvid = 0, rpid = 0;
        CFTypeRef cv = IORegistryEntryCreateCFProperty(svc, CFSTR("idVendor"), NULL, 0);
        CFTypeRef cp = IORegistryEntryCreateCFProperty(svc, CFSTR("idProduct"), NULL, 0);
        if (cv) { SInt32 t = 0; CFNumberGetValue(cv, kCFNumberSInt32Type, &t); rvid = (UInt16)t; CFRelease(cv); }
        if (cp) { SInt32 t = 0; CFNumberGetValue(cp, kCFNumberSInt32Type, &t); rpid = (UInt16)t; CFRelease(cp); }
        int isAIC = (rvid == VID) || (rvid == 0x368b);
        kern_return_t pr = IOCreatePlugInInterfaceForService(svc, kIOUSBDeviceUserClientTypeID,
                kIOCFPlugInInterfaceID, &plugin, &score);
        if (pr != KERN_SUCCESS && isAIC) LOG("  AIC device %04x:%04x: IOCreatePlugInInterfaceForService -> 0x%08x (owned by a kernel driver / other process?)", rvid, rpid, pr);
        if (pr == KERN_SUCCESS && plugin) {
            HRESULT qr = (*plugin)->QueryInterface(plugin,
                CFUUIDGetUUIDBytes(kIOUSBDeviceInterfaceID500), (LPVOID *)&d);
            (*plugin)->Release(plugin);
            if (qr != 0) LOG("  QueryInterface -> 0x%lx", (long)qr);
        }
        IOObjectRelease(svc);
        if (d) {
            UInt16 vid = 0, pid = 0;
            (*d)->GetDeviceVendor(d, &vid);
            (*d)->GetDeviceProduct(d, &pid);
            if (vid == VID || vid == 0x368b) LOG("  AIC device %04x:%04x", vid, pid);
            if ((vid == VID && (pid == 0x8d81 || pid == 0x8d83)) || (vid == 0x368b && pid == 0x8d85)) { *pidOut = pid; dev = d; break; }
            (*d)->Release(d);
        }
    }
    LOG("openDevice: %d USB device(s) enumerated", seen);
    IOObjectRelease(iter);
    return dev;
}

static int openInterface(IOUSBDeviceInterface500 **dev) {
    IOUSBFindInterfaceRequest req = {
        .bInterfaceClass = kIOUSBFindInterfaceDontCare,
        .bInterfaceSubClass = kIOUSBFindInterfaceDontCare,
        .bInterfaceProtocol = kIOUSBFindInterfaceDontCare,
        .bAlternateSetting = kIOUSBFindInterfaceDontCare,
    };
    io_iterator_t iter = 0;
    if ((*dev)->CreateInterfaceIterator(dev, &req, &iter) != kIOReturnSuccess) return -1;

    io_service_t svc;
    while ((svc = IOIteratorNext(iter))) {
        IOCFPlugInInterface **plugin = NULL;
        SInt32 score = 0;
        if (IOCreatePlugInInterfaceForService(svc, kIOUSBInterfaceUserClientTypeID,
                kIOCFPlugInInterfaceID, &plugin, &score) == KERN_SUCCESS && plugin) {
            (*plugin)->QueryInterface(plugin,
                CFUUIDGetUUIDBytes(kIOUSBInterfaceInterfaceID500), (LPVOID *)&gIntf);
            (*plugin)->Release(plugin);
        }
        IOObjectRelease(svc);
        if (gIntf) break;
    }
    IOObjectRelease(iter);
    if (!gIntf) return -1;

    IOReturn r = (*gIntf)->USBInterfaceOpen(gIntf);
    if (r != kIOReturnSuccess) { LOG("USBInterfaceOpen -> 0x%08x", r); return -1; }

    UInt8 nep = 0;
    (*gIntf)->GetNumEndpoints(gIntf, &nep);
    for (UInt8 pipe = 1; pipe <= nep; pipe++) {
        UInt8 dir = 0, num = 0, tt = 0, interval = 0;
        UInt16 mps = 0;
        if ((*gIntf)->GetPipeProperties(gIntf, pipe, &dir, &num, &tt, &mps, &interval) != kIOReturnSuccess)
            continue;
        if (tt != kUSBBulk) continue;
        if (dir == kUSBIn && gInN < 4) { gIn[gInN++] = pipe; LOG("bulk IN  pipe %u (ep %u, mps %u)", pipe, num, mps); }
        else if (dir == kUSBOut && gOutN < 4) { gOut[gOutN++] = pipe; LOG("bulk OUT pipe %u (ep %u, mps %u)", pipe, num, mps); }
    }
    return (gInN && gOutN) ? 0 : -1;
}


// openInterfaceDirect finds the IOUSBHostInterface for an AIC device straight
// from the registry (no device-level user client needed — that was refused
// with kIOReturnUnsupported for the 368b:8d85 firmware) and opens it.
static int openInterfaceDirect(void) {
    CFMutableDictionaryRef match = IOServiceMatching("IOUSBHostInterface");
    io_iterator_t iter = 0;
    if (IOServiceGetMatchingServices(kIOMainPortDefault, match, &iter) != KERN_SUCCESS) return -1;
    io_service_t svc;
    while ((svc = IOIteratorNext(iter))) {
        UInt16 rvid = 0, rpid = 0;
        CFTypeRef cv = IORegistryEntryCreateCFProperty(svc, CFSTR("idVendor"), NULL, 0);
        CFTypeRef cp = IORegistryEntryCreateCFProperty(svc, CFSTR("idProduct"), NULL, 0);
        if (cv) { SInt32 t = 0; CFNumberGetValue(cv, kCFNumberSInt32Type, &t); rvid = (UInt16)t; CFRelease(cv); }
        if (cp) { SInt32 t = 0; CFNumberGetValue(cp, kCFNumberSInt32Type, &t); rpid = (UInt16)t; CFRelease(cp); }
        int isAIC = (rvid == VID && (rpid == 0x8d81 || rpid == 0x8d83)) || (rvid == 0x368b && rpid == 0x8d85);
        if (!isAIC) { IOObjectRelease(svc); continue; }
        IOCFPlugInInterface **plugin = NULL; SInt32 score = 0;
        kern_return_t pr = IOCreatePlugInInterfaceForService(svc, kIOUSBInterfaceUserClientTypeID,
                kIOCFPlugInInterfaceID, &plugin, &score);
        LOG("interface %04x:%04x: IOCreatePlugInInterfaceForService -> 0x%08x", rvid, rpid, pr);
        if (pr == KERN_SUCCESS && plugin) {
            (*plugin)->QueryInterface(plugin, CFUUIDGetUUIDBytes(kIOUSBInterfaceInterfaceID500), (LPVOID *)&gIntf);
            (*plugin)->Release(plugin);
        }
        IOObjectRelease(svc);
        if (gIntf) break;
    }
    IOObjectRelease(iter);
    if (!gIntf) return -1;
    IOReturn r = (*gIntf)->USBInterfaceOpen(gIntf);
    if (r != kIOReturnSuccess) { LOG("USBInterfaceOpen -> 0x%08x", r); return -1; }
    UInt8 nep = 0; (*gIntf)->GetNumEndpoints(gIntf, &nep);
    LOG("interface open: %u endpoints", nep);
    for (UInt8 pipe = 1; pipe <= nep; pipe++) {
        UInt8 dir = 0, num = 0, tt = 0, interval = 0; UInt16 mps = 0;
        if ((*gIntf)->GetPipeProperties(gIntf, pipe, &dir, &num, &tt, &mps, &interval) != kIOReturnSuccess) continue;
        LOG("  pipe %u: ep %u dir=%s type=%u mps=%u", pipe, num, dir==kUSBIn?"IN":"OUT", tt, mps);
        if (tt != kUSBBulk) continue;
        if (dir == kUSBIn && gInN < 4) gIn[gInN++] = pipe;
        else if (dir == kUSBOut && gOutN < 4) gOut[gOutN++] = pipe;
    }
    return (gInN && gOutN) ? 0 : -1;
}

int main(int argc, char **argv) {
    for (int i = 1; i < argc; i++) if (!strcmp(argv[i], "nostack")) gDoStack = 0;
    uint16_t pid = 0;
    IOUSBDeviceInterface500 **dev = openDevice(&pid);
    if (!dev) {
        LOG("device-level user client unavailable; opening the interface directly...");
        if (openInterfaceDirect() != 0) { LOG("no operational AIC8800D80 found (a69c:8d81/8d83 or 368b:8d85)."); return 1; }
        runBringup();
        (*gIntf)->USBInterfaceClose(gIntf);
        return 0;
    }
    LOG("found operational device pid=0x%04x", pid);

    IOReturn r = (*dev)->USBDeviceOpen(dev);
    if (r != kIOReturnSuccess) { LOG("USBDeviceOpen -> 0x%08x (another driver may own it)", r); return 1; }
    (*dev)->SetConfiguration(dev, 1);

    if (openInterface(dev) != 0) { LOG("could not open interface / find bulk pipes"); return 1; }
    LOG("interface open: %u IN pipe(s), %u OUT pipe(s)", gInN, gOutN);

    runBringup();

    (*gIntf)->USBInterfaceClose(gIntf);
    (*dev)->USBDeviceClose(dev);
    return 0;
}
