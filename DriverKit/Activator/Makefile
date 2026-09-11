# Assembles EventHorizonActivator.app — the host bundle that activates the
# AIC8800D80 dext via OSSystemExtensionRequest (macOS has no CLI to load a
# DriverKit system extension; it must come from a signed app bundle).
#
# Bundle layout:
#   EventHorizonActivator.app/
#     Contents/Info.plist
#     Contents/MacOS/EventHorizonActivator          (this Swift tool)
#     Contents/Library/SystemExtensions/AIC8800D80Driver.dext
#
# Signing: ad-hoc by default (IDENTITY=-), which is what the local dev path
# needs (SIP + AMFI disabled, `systemextensionsctl developer on`). For the
# clean path — SIP left on — override IDENTITY with a Developer ID cert whose
# team holds the Apple-granted DriverKit entitlement, and PROFILE with the
# matching provisioning profile:
#   make IDENTITY="Developer ID Application: … (TEAMID)" PROFILE=driverkit.provisionprofile

APP        := EventHorizonActivator.app
BIN        := EventHorizonActivator
DEXT_SRC   := ../AIC8800D80/build/AIC8800D80Driver.dext
DEXT_NAME  := AIC8800D80Driver.dext
IDENTITY   ?= -
PROFILE    ?=
TARGET     := arm64-apple-macos13.0

.PHONY: all clean dext activate deactivate

all: $(APP)

# Build the dext via its own Makefile first.
dext:
	$(MAKE) -C ../AIC8800D80

$(BIN): main.swift
	swiftc -target $(TARGET) -framework SystemExtensions main.swift -o $(BIN)

$(APP): $(BIN) Info.plist Activator.entitlements dext
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS
	mkdir -p $(APP)/Contents/Library/SystemExtensions
	cp Info.plist $(APP)/Contents/Info.plist
	cp $(BIN) $(APP)/Contents/MacOS/$(BIN)
	cp -R $(DEXT_SRC) $(APP)/Contents/Library/SystemExtensions/$(DEXT_NAME)
ifneq ($(PROFILE),)
	cp $(PROFILE) $(APP)/Contents/embedded.provisionprofile
endif
	# Sign inner-most first: the embedded dext, then the app.
	codesign --force --sign "$(IDENTITY)" \
	    --entitlements ../AIC8800D80/AIC8800D80Driver.entitlements \
	    $(APP)/Contents/Library/SystemExtensions/$(DEXT_NAME)
	codesign --force --sign "$(IDENTITY)" \
	    --entitlements Activator.entitlements \
	    $(APP)
	@echo "built $(APP) (signed with identity: $(IDENTITY))"
	@codesign -dv $(APP) 2>&1 | grep -E "Identifier|TeamIdentifier|Signature" || true

activate: $(APP)
	./$(APP)/Contents/MacOS/$(BIN) activate

deactivate: $(APP)
	./$(APP)/Contents/MacOS/$(BIN) deactivate

clean:
	rm -rf $(APP) $(BIN)
