#!/bin/bash
set -eo pipefail

echo "================================================================"
echo "🧪 Running Event Horizon Ginkgo End-to-End (E2E) Test Suite"
echo "================================================================"

# Check if daemon is running, start in background if not
if ! curl -s http://127.0.0.1:8990/api/status > /dev/null; then
    echo "⚙️ Daemon not detected on :8990. Starting test daemon..."
    go build -buildvcs=false -o bin/usbwifi ./cmd/usbwifi
    sudo ./bin/usbwifi --port 8990 &
    DAEMON_PID=$!
    trap "sudo kill -9 $DAEMON_PID 2>/dev/null || true" EXIT
    sleep 2
fi

echo "🚀 Executing Ginkgo E2E Specifications..."
go test -v -buildvcs=false -timeout 120s ./e2e/...

echo "================================================================"
echo "🎉 All E2E Integration & Verification Tests Passed Successfully!"
echo "================================================================"
