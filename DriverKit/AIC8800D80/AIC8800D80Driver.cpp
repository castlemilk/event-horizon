// AIC8800D80Driver.cpp
//
// In-kernel DriverKit driver for the AIC8800D80 USB Wi-Fi 6 chipset.
//
// The whole reason this exists: the LMAC bring-up requires MM_SET_STACK_START,
// which HALTS the bulk pipes. User-space libusb on macOS cannot recover a
// halted pipe (clear_halt returns LIBUSB_ERROR_OTHER; the session is lost), so
// the Go port could load firmware and speak every control message but never
// brought the radio online. An IOUSBHostPipe CAN ClearStall/Abort the pipe at
// the IOKit level, so this driver runs the LMAC init IN-KERNEL and recovers the
// pipes after stack_start.
//
// This first cut drives the sequence and logs the outcome via os_log; the
// IO80211 network-interface binding (enX) and the WPA2 supplicant are layered
// on top once the radio is confirmed alive.

#include "build/AIC8800D80Driver.h"

#include <os/log.h>
#include <DriverKit/IOLib.h>
#include <DriverKit/IOMemoryDescriptor.h>
#include <DriverKit/IOBufferMemoryDescriptor.h>
#include <USBDriverKit/IOUSBHostDevice.h>
#include <USBDriverKit/IOUSBHostInterface.h>
#include <USBDriverKit/IOUSBHostPipe.h>
#include <USBDriverKit/AppleUSBDefinitions.h>
#include <USBDriverKit/AppleUSBDescriptorParsing.h>
#include <USBDriverKit/USBDriverKitDefs.h>

#define LOG(fmt, ...) os_log(OS_LOG_DEFAULT, "AIC8800D80: " fmt, ##__VA_ARGS__)

// ---- LMAC protocol constants (see pkg/aic8800d80/lmac in the Go port) -------
static const uint16_t DRV_TASK_ID = 100;
enum { TASK_MM = 0, TASK_SCANU = 4, TASK_ME = 5, TASK_SM = 6 };

// Message ids verified on this firmware (Amlogic sub-wall build).
enum {
    MM_RESET_REQ         = 0x0000, MM_RESET_CFM         = 0x0001,
    MM_START_REQ         = 0x0002, MM_START_CFM         = 0x0003,
    MM_VERSION_REQ       = 0x0004, MM_VERSION_CFM       = 0x0005,
    MM_ADD_IF_REQ        = 0x0006, MM_ADD_IF_CFM        = 0x0007,
    MM_SET_COEX_REQ      = 0x0067,
    MM_SET_RF_CALIB_REQ  = 0x006B, MM_SET_RF_CALIB_CFM  = 0x006C,
    MM_SET_TXPWR_IDX_LVL_REQ = 0x0079,
    MM_SET_STACK_START_REQ   = 0x007B, MM_SET_STACK_START_CFM = 0x007C,
    ME_CONFIG_REQ        = 0x1400, ME_CONFIG_CFM        = 0x1401,
    ME_CHAN_CONFIG_REQ   = 0x1402, ME_CHAN_CONFIG_CFM   = 0x1403,
    SCANU_START_REQ      = 0x1000,
};

struct AIC8800D80Driver_IVars {
    IOUSBHostDevice*    device;
    IOUSBHostInterface* interface;
    IOUSBHostPipe*      bulkIn;   // data/response pipe
    IOUSBHostPipe*      bulkOut;  // command pipe
    uint8_t             bulkInAddr;
    uint8_t             bulkOutAddr;
    IOBufferMemoryDescriptor* txBuf;
    IOBufferMemoryDescriptor* rxBuf;
    uint8_t*            txPtr;
    uint8_t*            rxPtr;
    uint16_t            seq;
};

// -----------------------------------------------------------------------------

bool
AIC8800D80Driver::init()
{
    if (!IOService::init()) {
        return false;
    }
    ivars = IONewZero(AIC8800D80Driver_IVars, 1);
    return ivars != nullptr;
}

void
AIC8800D80Driver::free()
{
    if (ivars) {
        IOSafeDeleteNULL(ivars, AIC8800D80Driver_IVars, 1);
    }
    IOService::free();
}

// wrapCmd builds the on-wire command frame in ivars->txBuf:
//   [len:2 LE = paramLen+12][0x11][0x00][dummy u32=0]
//   [id:2][dest:2][src=seq:2][paramLen:2][param...]
// and returns the total byte length. Mirrors lmac.WrapCommand + the submitter's
// per-call seq injected into src_id.
static uint32_t buildCmd(AIC8800D80Driver_IVars* v, uint16_t id, uint16_t dest,
                         const uint8_t* param, uint16_t paramLen)
{
    uint8_t* p = v->txPtr;
    uint16_t rec = (uint16_t)(paramLen + 12);
    p[0] = rec & 0xff;
    p[1] = (rec >> 8) & 0x0f;
    p[2] = 0x11;
    p[3] = 0x00;
    p[4] = p[5] = p[6] = p[7] = 0; // dummy word
    // lmac_msg header at offset 8
    p[8]  = id & 0xff;   p[9]  = (id >> 8) & 0xff;
    p[10] = dest & 0xff; p[11] = (dest >> 8) & 0xff;
    uint16_t s = ++v->seq;
    p[12] = s & 0xff;    p[13] = (s >> 8) & 0xff;
    p[14] = paramLen & 0xff; p[15] = (paramLen >> 8) & 0xff;
    for (uint16_t i = 0; i < paramLen; i++) {
        p[16 + i] = param ? param[i] : 0;
    }
    return (uint32_t)(16 + paramLen);
}

// sendCmd writes one command to the bulk OUT pipe (synchronous).
static kern_return_t sendCmd(AIC8800D80Driver_IVars* ivars, uint16_t id, uint16_t dest,
                             const uint8_t* param, uint16_t paramLen)
{
    uint32_t total = buildCmd(ivars, id, dest, param, paramLen);
    ivars->txBuf->SetLength(total);
    uint32_t sent = 0;
    kern_return_t r = ivars->bulkOut->IO(ivars->txBuf, total, &sent, 1000);
    if (r != kIOReturnSuccess) {
        LOG("sendCmd id=0x%04x: bulk OUT err 0x%x", id, r);
    }
    return r;
}

// waitCfm reads response frames from the bulk IN pipe until one whose lmac id is
// in the same task as wantTask arrives, or the deadline passes. Returns
// kIOReturnSuccess on match. The RX record is [len:2][type:1][pad:1] then the
// ipc_e2a_msg (id@0, ..., param_len@6, pattern@8, param@12).
static kern_return_t waitCfm(AIC8800D80Driver_IVars* ivars, uint16_t wantId, int tries)
{
    uint16_t wantTask = wantId & 0xFC00;
    for (int i = 0; i < tries; i++) {
        ivars->rxBuf->SetLength(4096);
        uint32_t got = 0;
        kern_return_t r = ivars->bulkIn->IO(ivars->rxBuf, 4096, &got, 300);
        if (r != kIOReturnSuccess || got < 4) {
            continue;
        }
        uint8_t* d = ivars->rxPtr;
        uint8_t type = d[2];
        if ((type & 0x10) == 0) {
            continue; // data frame, not a config cfm
        }
        if (got < 16) {
            continue;
        }
        uint16_t id = (uint16_t)(d[4] | (d[5] << 8)); // ipc_e2a_msg id is at record offset 4
        if ((id & 0xFC00) == wantTask) {
            return kIOReturnSuccess;
        }
    }
    return kIOReturnTimeout;
}

// -----------------------------------------------------------------------------

// runBringup executes the LMAC init sequence with the stack_start pipe recovery.
static kern_return_t runBringup(AIC8800D80Driver_IVars* ivars)
{
    static const uint8_t stackStart[4] = {0x01, 0x00, 0x20, 0x00};
    static uint8_t zero112[112] = {0};
    static const uint8_t coex[16]  = {1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};

    // 1. Start the MAC/PHY stack. This halts the pipes; recover them next.
    LOG("stack_start...");
    sendCmd(ivars, MM_SET_STACK_START_REQ, TASK_MM, stackStart, 4);
    waitCfm(ivars, MM_SET_STACK_START_CFM, 20);
    IOSleep(1500);
    // *** The key operation user-space libusb could not do on macOS: ***
    ivars->bulkOut->ClearStall(true);
    ivars->bulkIn->ClearStall(true);
    IOSleep(300);

    // 2. RF calibration (txpwr + rf_calib). Payloads: txpwr is a 95-byte union
    //    with the v3 levels; here we send a conservative all-18dBm set. rf_calib
    //    carries the D81 constants.
    uint8_t txpwr[95] = {0};
    txpwr[0] = 1; // enable
    for (int i = 1; i < 69; i++) txpwr[i] = 18; // per-rate levels (dBm)
    if (sendCmd(ivars, MM_SET_TXPWR_IDX_LVL_REQ, TASK_MM, txpwr, 95) == kIOReturnSuccess) {
        waitCfm(ivars, MM_SET_TXPWR_IDX_LVL_REQ + 1, 10);
    }
    uint8_t rfcal[24] = {0};
    // cal_cfg_24g=0x0f8f, cal_cfg_5g=0x0f0f, param_alpha=0x0c34c008, bt_calib_param=0x264203
    rfcal[0]=0x8f; rfcal[1]=0x0f; rfcal[4]=0x0f; rfcal[5]=0x0f;
    rfcal[8]=0x08; rfcal[9]=0xc0; rfcal[10]=0x34; rfcal[11]=0x0c;
    rfcal[16]=0x03; rfcal[17]=0x42; rfcal[18]=0x26;
    if (sendCmd(ivars, MM_SET_RF_CALIB_REQ, TASK_MM, rfcal, 24) == kIOReturnSuccess) {
        waitCfm(ivars, MM_SET_RF_CALIB_CFM, 10);
    }

    // 3. MAC init: reset -> me_config -> me_chan_config -> start -> coex -> add_if.
    if (sendCmd(ivars, MM_RESET_REQ, TASK_MM, nullptr, 0) != kIOReturnSuccess) return kIOReturnError;
    if (waitCfm(ivars, MM_RESET_CFM, 20) != kIOReturnSuccess) { LOG("reset: no cfm (pipe recovery failed?)"); return kIOReturnError; }
    LOG("reset ok - pipes survived stack_start");

    sendCmd(ivars, ME_CONFIG_REQ, TASK_ME, zero112, 112);
    waitCfm(ivars, ME_CONFIG_CFM, 10);

    uint8_t chanCfg[254] = {0};
    for (uint8_t ch = 1; ch <= 13; ch++) {
        uint16_t freq = 2407 + (uint16_t)ch * 5;
        int off = (ch - 1) * 6;
        chanCfg[off] = freq & 0xff; chanCfg[off+1] = (freq >> 8) & 0xff;
        chanCfg[off+2] = 0; chanCfg[off+4] = 20;
    }
    chanCfg[252] = 13; // chan2G4_cnt
    sendCmd(ivars, ME_CHAN_CONFIG_REQ, TASK_ME, chanCfg, 254);
    waitCfm(ivars, ME_CHAN_CONFIG_CFM, 10);

    uint8_t startReq[72] = {0};
    startReq[64] = 0x2c; startReq[65] = 0x01; // uapsd_timeout=300
    startReq[68] = 20;                          // lp_clk_accuracy
    sendCmd(ivars, MM_START_REQ, TASK_MM, startReq, 72);
    waitCfm(ivars, MM_START_CFM, 10);

    sendCmd(ivars, MM_SET_COEX_REQ, TASK_MM, coex, 16);
    waitCfm(ivars, MM_SET_COEX_REQ + 1, 10);

    uint8_t addIf[10] = {0};      // type=MM_STA(0), addr@2 (LAA), p2p@8
    addIf[2]=0x02; addIf[3]=0x11; addIf[4]=0x22; addIf[5]=0x33; addIf[6]=0x44; addIf[7]=0x55;
    sendCmd(ivars, MM_ADD_IF_REQ, TASK_MM, addIf, 10);
    waitCfm(ivars, MM_ADD_IF_CFM, 10);
    LOG("station interface up (vif) - init complete");

    // 4. Scan and log any beacons (the proof the radio is alive).
    uint8_t scan[376] = {0};
    uint8_t chans[3] = {1, 6, 11};
    for (int i = 0; i < 3; i++) {
        uint16_t freq = 2407 + (uint16_t)chans[i] * 5;
        int off = i * 6;
        scan[off] = freq & 0xff; scan[off+1] = (freq >> 8) & 0xff;
        scan[off+2] = 0; scan[off+4] = 20;
    }
    for (int i = 0; i < 6; i++) scan[352 + i] = 0xff; // broadcast bssid
    scan[366] = 0; // vif_idx
    scan[367] = 3; // chan_cnt
    LOG("scanning...");
    sendCmd(ivars, SCANU_START_REQ, TASK_SCANU, scan, 376);

    int beacons = 0;
    for (int i = 0; i < 60; i++) { // ~18s of RX polling
        ivars->rxBuf->SetLength(4096);
        uint32_t got = 0;
        if (ivars->bulkIn->IO(ivars->rxBuf, 4096, &got, 300) != kIOReturnSuccess || got < 60) {
            continue;
        }
        uint8_t* d = ivars->rxPtr;
        // data frame carrying an 802.11 mgmt MPDU at record offset 60
        if ((d[2] & 0x10) == 0 && got >= 60 + 38) {
            uint8_t* m = d + 60;
            if (m[0] == 0x80 || m[0] == 0x50) { // beacon / probe-resp
                beacons++;
                LOG("beacon: bssid %02x:%02x:%02x:%02x:%02x:%02x",
                    m[16], m[17], m[18], m[19], m[20], m[21]);
            }
        }
    }
    LOG("scan complete: %d beacons seen", beacons);
    return kIOReturnSuccess;
}

// -----------------------------------------------------------------------------

// setupPipes opens the interface and resolves the bulk IN/OUT endpoints.
static kern_return_t setupPipes(AIC8800D80Driver* self, AIC8800D80Driver_IVars* ivars)
{
    kern_return_t r = ivars->interface->Open(self, 0, 0);
    if (r != kIOReturnSuccess) { LOG("interface Open: 0x%x", r); return r; }

    const IOUSBConfigurationDescriptor* cfg = ivars->interface->CopyConfigurationDescriptor();
    if (!cfg) { LOG("no config descriptor"); return kIOReturnError; }
    const IOUSBInterfaceDescriptor* ifd = ivars->interface->GetInterfaceDescriptor(cfg);
    const IOUSBDescriptorHeader* cur = (const IOUSBDescriptorHeader*)ifd;
    const IOUSBEndpointDescriptor* ep = nullptr;
    while ((ep = IOUSBGetNextEndpointDescriptor(cfg, ifd, cur)) != nullptr) {
        cur = (const IOUSBDescriptorHeader*)ep;
        if ((ep->bmAttributes & 0x03) != 0x02) continue; // bulk only
        if (ep->bEndpointAddress & 0x80) {
            if (ivars->bulkInAddr == 0) ivars->bulkInAddr = ep->bEndpointAddress;
        } else {
            if (ivars->bulkOutAddr == 0) ivars->bulkOutAddr = ep->bEndpointAddress;
        }
    }
    IOUSBHostFreeDescriptor(cfg);
    if (!ivars->bulkInAddr || !ivars->bulkOutAddr) {
        LOG("bulk endpoints not found (in=0x%02x out=0x%02x)", ivars->bulkInAddr, ivars->bulkOutAddr);
        return kIOReturnError;
    }
    LOG("bulk endpoints in=0x%02x out=0x%02x", ivars->bulkInAddr, ivars->bulkOutAddr);

    r = ivars->interface->CopyPipe(ivars->bulkInAddr, &ivars->bulkIn);
    if (r != kIOReturnSuccess) { LOG("CopyPipe in: 0x%x", r); return r; }
    r = ivars->interface->CopyPipe(ivars->bulkOutAddr, &ivars->bulkOut);
    if (r != kIOReturnSuccess) { LOG("CopyPipe out: 0x%x", r); return r; }
    return kIOReturnSuccess;
}

kern_return_t
IMPL(AIC8800D80Driver, Start)
{
    kern_return_t r = Start(provider, SUPERDISPATCH);
    if (r != kIOReturnSuccess) return r;

    ivars->device = OSDynamicCast(IOUSBHostDevice, provider);
    if (!ivars->device) { LOG("provider not IOUSBHostDevice"); return kIOReturnUnsupported; }
    ivars->device->retain();

    r = ivars->device->Open(this, 0, 0);
    if (r != kIOReturnSuccess) { LOG("device Open: 0x%x", r); return r; }

    ivars->device->SetConfiguration(1, false);

    // Grab interface 0.
    r = ivars->device->CopyInterface(0, &ivars->interface);
    if (r != kIOReturnSuccess || !ivars->interface) { LOG("CopyInterface: 0x%x", r); return kIOReturnError; }

    r = setupPipes(this, ivars);
    if (r != kIOReturnSuccess) return r;

    // Allocate the TX/RX DMA buffers once.
    if (IOBufferMemoryDescriptor::Create(kIOMemoryDirectionOut, 4096, 0, &ivars->txBuf) != kIOReturnSuccess ||
        IOBufferMemoryDescriptor::Create(kIOMemoryDirectionIn, 4096, 0, &ivars->rxBuf) != kIOReturnSuccess) {
        LOG("buffer alloc failed");
        return kIOReturnNoMemory;
    }
    IOAddressSegment seg;
    ivars->txBuf->GetAddressRange(&seg); ivars->txPtr = (uint8_t*)seg.address;
    ivars->rxBuf->GetAddressRange(&seg); ivars->rxPtr = (uint8_t*)seg.address;

    LOG("started; running LMAC bring-up");
    runBringup(ivars);

    RegisterService();
    return kIOReturnSuccess;
}

kern_return_t
IMPL(AIC8800D80Driver, Stop)
{
    LOG("Stop");
    if (ivars->bulkIn)  { ivars->bulkIn->Abort(kIOUSBAbortSynchronous, kIOReturnAborted, nullptr); ivars->bulkIn->release(); ivars->bulkIn = nullptr; }
    if (ivars->bulkOut) { ivars->bulkOut->Abort(kIOUSBAbortSynchronous, kIOReturnAborted, nullptr); ivars->bulkOut->release(); ivars->bulkOut = nullptr; }
    if (ivars->txBuf) { ivars->txBuf->release(); ivars->txBuf = nullptr; }
    if (ivars->rxBuf) { ivars->rxBuf->release(); ivars->rxBuf = nullptr; }
    if (ivars->interface) { ivars->interface->Close(this, 0); ivars->interface->release(); ivars->interface = nullptr; }
    if (ivars->device) { ivars->device->Close(this, 0); ivars->device->release(); ivars->device = nullptr; }
    return Stop(provider, SUPERDISPATCH);
}
