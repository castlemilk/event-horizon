.PHONY: build package run test clean

APP_NAME=Starlink WiFi Manager
BUILD_DIR=build

build:
	@mkdir -p bin
	go build -o bin/usbwifi ./cmd/usbwifi
	swift build

package:
	@chmod +x scripts/package_app.sh
	./scripts/package_app.sh

run: package
	open "$(BUILD_DIR)/$(APP_NAME).app"

test:
	swift test

clean:
	rm -rf bin build .build
