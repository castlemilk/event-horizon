.PHONY: build package run test clean aic-loader aic-firmware aic-test

APP_NAME=Event Horizon
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

# ----- AIC8800D80 driver-stack workstreams -----

aic-test:
	go test ./pkg/aic8800d80/...

aic-loader:
	@mkdir -p bin
	go build -o bin/usbwifi ./cmd/usbwifi
	@echo "Built aicloader inside bin/usbwifi (run: sudo ./bin/usbwifi aicloader --help)"

aic-firmware:
	@mkdir -p bin
	go build -o bin/usbwifi ./cmd/usbwifi
	@echo "Built firmware subcommand inside bin/usbwifi (run: ./bin/usbwifi firmware --help)"

clean:
	rm -rf bin build .build
