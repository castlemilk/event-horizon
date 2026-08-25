# Makefile for the AIC8800D80 DriverKit driver.
#
# Output: DriverKit/AIC8800D80/build/AIC8800D80Driver.dext
#
# Loading the bundle requires:
#   1. A Developer ID Application certificate (already configured).
#   2. Approval via `systemextensionsctl developer on` on the target Mac.
#
# The driver is a skeleton — it matches the operational VID:PID of
# the AIC8800D80 and exposes the IOUSBHostDevice. It does NOT yet
# bind to the macOS 802.11 subsystem (requires private IO80211
# framework headers — see IO80211Controller.h).

DRIVERKIT_SDK := /Applications/Xcode.app/Contents/Developer/Platforms/DriverKit.platform/Developer/SDKs/DriverKit.sdk
DRIVERKIT_HEADERS := $(DRIVERKIT_SDK)/System/DriverKit/System/Library/Frameworks/DriverKit.framework/Headers
DRIVERKIT_PRIVATE_HEADERS := $(DRIVERKIT_SDK)/System/DriverKit/System/Library/Frameworks/DriverKit.framework/PrivateHeaders
USBDRIVERKIT_HEADERS := $(DRIVERKIT_SDK)/System/DriverKit/System/Library/Frameworks/USBDriverKit.framework/Headers
IIG := /Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/iig

CXX := clang++
CXXFLAGS := -std=c++17 -fno-exceptions -fno-rtti \
            -isysroot $(DRIVERKIT_SDK) \
            -isystem $(DRIVERKIT_HEADERS) \
            -isystem $(DRIVERKIT_PRIVATE_HEADERS) \
            -isystem $(USBDRIVERKIT_HEADERS) \
            -I$(BUILD_DIR) -I.

IIG_FLAGS := -x objective-c++ -fno-exceptions -fno-rtti -D__IIG \
              -isysroot $(DRIVERKIT_SDK) \
              -I$(DRIVERKIT_HEADERS) \
              -I$(DRIVERKIT_PRIVATE_HEADERS) \
              -I$(USBDRIVERKIT_HEADERS)

BUILD_DIR := build
DRIVER_NAME := AIC8800D80Driver
DEXT_BUNDLE := $(BUILD_DIR)/$(DRIVER_NAME).dext

# Set IDENTITY to your Developer ID Application certificate common name.
# Find it with: security find-identity -v -p codesigning /Library/Keychains/System.keychain
IDENTITY ?= Developer ID Application: Ben Ebsworth (WFTX6CN23F)

.PHONY: all clean install uninstall

all: $(DEXT_BUNDLE)

# Generate the C++ header from the .iig file via the iig tool.
# iig requires -D__IIG so KERNEL macros resolve correctly.
$(BUILD_DIR)/%.h: %.iig | $(BUILD_DIR)
	$(IIG) --def $< --header $(BUILD_DIR)/$*.h --impl $(BUILD_DIR)/$*_impl.h \
	       --framework-name DriverKit --deployment-target 13.0 -- \
	       $(IIG_FLAGS)

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(BUILD_DIR)/%.o: $(BUILD_DIR)/%.h %.cpp | $(BUILD_DIR)
	$(CXX) $(CXXFLAGS) -c $(filter %.cpp,$^) -o $@

$(DEXT_BUNDLE): $(BUILD_DIR)/$(DRIVER_NAME).o $(BUILD_DIR)/$(DRIVER_NAME).h Info.plist
	@mkdir -p $(BUILD_DIR)
	$(CXX) -bundle \
            -isysroot $(DRIVERKIT_SDK) \
            -framework DriverKit -framework USBDriverKit \
            -Wl,-dylib_install_name,/System/Extensions/$(DRIVER_NAME).dext/$(DRIVER_NAME) \
            -Wl,-install_name,@rpath/$(DRIVER_NAME).dext/$(DRIVER_NAME) \
            $(BUILD_DIR)/$(DRIVER_NAME).o -o $(BUILD_DIR)/$(DRIVER_NAME)
	@mkdir -p $(DEXT_BUNDLE)
	cp $(BUILD_DIR)/$(DRIVER_NAME) $(DEXT_BUNDLE)/$(DRIVER_NAME)
	cp Info.plist $(DEXT_BUNDLE)/Info.plist
	codesign --force --options runtime \
            --entitlements AIC8800D80Driver.entitlements \
            --sign "$(IDENTITY)" \
            $(DEXT_BUNDLE)/$(DRIVER_NAME)
	codesign --force \
            --entitlements AIC8800D80Driver.entitlements \
            --sign "$(IDENTITY)" \
            $(DEXT_BUNDLE)

install: $(DEXT_BUNDLE)
	sudo cp -R $(DEXT_BUNDLE) /Library/SystemExtensions/
	sudo systemextensionsctl developer on
	sudo systemextensionsctl load com.eventhorizon.driver.AIC8800D80
	systemextensionsctl list | grep -i aic || true

uninstall:
	sudo systemextensionsctl unload com.eventhorizon.driver.AIC8800D80 || true
	sudo rm -rf /Library/SystemExtensions/$(DRIVER_NAME).dext

clean:
	rm -rf $(BUILD_DIR)
