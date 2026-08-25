// AIC8800D80Driver.cpp
//
// DriverKit driver for the AIC8800D80 USB Wi-Fi 6 chipset. Skeleton
// implementation that matches the operational VID:PID (0xa69c:0x8d81 /
// 0xa69c:0x8d83) and exposes the device to a user-space client over
// IOUSBHostInterface / IOUSBHostPipe. The actual 802.11 binding is
// not yet implemented (requires private IO80211 headers — see
// IO80211Controller.h in /Users/benebsworth/projects/event-horizon/DriverKit/AIC8800D80/).

#include "build/AIC8800D80Driver.h"

#include <os/log.h>
#include <DriverKit/IOUserServer.h>
#include <DriverKit/IOLib.h>
#include <USBDriverKit/IOUSBHostDevice.h>

#define AIC8800D80_VENDOR      0xa69c
#define AIC8800D80_PID_WIFI_BT 0x8d81
#define AIC8800D80_PID_WIFI    0x8d83

kern_return_t
IMPL(AIC8800D80Driver, Start)
{
    kern_return_t ret = kIOReturnSuccess;
    os_log(OS_LOG_DEFAULT, "AIC8800D80: Start (matched VID 0x%04x, PIDs 0x%04x/0x%04x)",
        AIC8800D80_VENDOR, AIC8800D80_PID_WIFI_BT, AIC8800D80_PID_WIFI);

    IOUSBHostDevice* device = OSDynamicCast(IOUSBHostDevice, provider);
    if (device == nullptr) {
        os_log(OS_LOG_DEFAULT, "AIC8800D80: provider is not IOUSBHostDevice");
        return kIOReturnUnsupported;
    }

    // Open the device for clients in user-space.
    ret = device->Open(this, 0, 0);
    if (ret != kIOReturnSuccess) {
        os_log(OS_LOG_DEFAULT, "AIC8800D80: device open failed: 0x%x", ret);
        return ret;
    }

    // Advertise the service so a user-space client (e.g. the firmware
    // loader) can connect via IOUserClient and submit bulk I/O.
    RegisterService();
    os_log(OS_LOG_DEFAULT, "AIC8800D80: started; ready for IOUserClient / IO80211 binding");
    return kIOReturnSuccess;
}

kern_return_t
IMPL(AIC8800D80Driver, Stop)
{
    os_log(OS_LOG_DEFAULT, "AIC8800D80: Stop");
    return Stop(provider, SUPERDISPATCH);
}
