// driver subcommand — wraps the DriverKit driver install/uninstall flow.
//
// Usage:
//   sudo ./bin/usbwifi driver install
//   sudo ./bin/usbwifi driver uninstall
//   ./bin/usbwifi driver status
//
// This is the entry point for end users. It compiles the DriverKit
// driver (if needed), signs it, copies it to /Library/SystemExtensions,
// and registers it with systemextensionsctl.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	driverBundleID = "com.eventhorizon.driver.AIC8800D80"
	driverRelPath  = "DriverKit/AIC8800D80/build/AIC8800D80Driver.dext"
	driverExtDir   = "/Library/SystemExtensions"
)

func runDriverCmd(args []string) int {
	if len(args) < 1 {
		usageDriver()
		return 1
	}
	switch args[0] {
	case "install":
		return runDriverInstall()
	case "uninstall":
		return runDriverUninstall()
	case "status":
		return runDriverStatus()
	case "build":
		return runDriverBuild()
	case "help", "-h", "--help":
		usageDriver()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown driver subcommand: %s\n", args[0])
		usageDriver()
		return 1
	}
}

// runDriverBuild compiles the DriverKit driver into a .dext bundle.
func runDriverBuild() int {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return 1
	}
	driverDir := filepath.Join(repoRoot, "DriverKit", "AIC8800D80")
	makeFile := filepath.Join(driverDir, "Makefile")
	if _, err := os.Stat(makeFile); err != nil {
		log.Printf("driver build: %s not found", makeFile)
		return 1
	}
	log.Printf("building DriverKit driver in %s", driverDir)
	cmd := exec.Command("make", "-C", driverDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		log.Printf("driver build failed: %v", err)
		return 1
	}
	log.Printf("driver build OK")
	return 0
}

// runDriverInstall builds, copies, and loads the DriverKit driver.
// Requires sudo for the SystemExtensions copy and systemextensionsctl
// registration.
func runDriverInstall() int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "driver install requires sudo. Re-run with: sudo ./bin/usbwifi driver install")
		return 1
	}

	// 1. Build the driver.
	log.Printf("[1/4] building DriverKit driver...")
	if rc := runDriverBuild(); rc != 0 {
		return rc
	}

	// 2. Verify the build artifact exists.
	repoRoot, err := findRepoRoot()
	if err != nil {
		return 1
	}
	bundleSrc := filepath.Join(repoRoot, driverRelPath)
	if _, err := os.Stat(bundleSrc); err != nil {
		log.Printf("driver bundle not found at %s", bundleSrc)
		log.Printf("build output: %s", bundleSrc)
		return 1
	}

	// 3. Make sure SystemExtensions dir exists.
	if _, err := os.Stat(driverExtDir); err != nil {
		log.Printf("creating %s", driverExtDir)
		if err := os.MkdirAll(driverExtDir, 0o755); err != nil {
			log.Printf("mkdir %s: %v", driverExtDir, err)
			return 1
		}
	}

	// 4. Remove any prior copy.
	bundleDst := filepath.Join(driverExtDir, "AIC8800D80Driver.dext")
	_ = os.RemoveAll(bundleDst)

	// 5. Copy the bundle.
	log.Printf("[2/4] copying %s -> %s", bundleSrc, bundleDst)
	cmd := exec.Command("cp", "-R", bundleSrc, bundleDst)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("cp failed: %v\n%s", err, out)
		return 1
	}

	// 6. Enable developer mode (so unsigned/Developer ID drivers can load).
	log.Printf("[3/4] enabling developer mode (systemextensionsctl developer on)")
	_ = runCmd("systemextensionsctl", "developer", "on")

	// 7. Load the driver.
	log.Printf("[4/4] loading %s", driverBundleID)
	if out, err := exec.Command("systemextensionsctl", "load", driverBundleID).CombinedOutput(); err != nil {
		log.Printf("systemextensionsctl load failed: %v\n%s", err, out)
		log.Printf("you may need to open System Settings > Privacy & Security and approve the blocked extension")
		return 1
	}

	log.Printf("driver installed. status:")
	_ = runDriverStatus()
	return 0
}

// runDriverUninstall removes the DriverKit driver.
func runDriverUninstall() int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "driver uninstall requires sudo. Re-run with: sudo ./bin/usbwifi driver uninstall")
		return 1
	}
	log.Printf("unloading %s", driverBundleID)
	_ = runCmd("systemextensionsctl", "unload", driverBundleID)

	bundleDst := filepath.Join(driverExtDir, "AIC8800D80Driver.dext")
	if _, err := os.Stat(bundleDst); err == nil {
		log.Printf("removing %s", bundleDst)
		if err := os.RemoveAll(bundleDst); err != nil {
			log.Printf("remove %s: %v", bundleDst, err)
			return 1
		}
	}
	log.Printf("driver uninstalled")
	return 0
}

// runDriverStatus reports whether the driver is built and whether
// systemextensionsctl thinks it's loaded.
func runDriverStatus() int {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return 1
	}
	bundleSrc := filepath.Join(repoRoot, driverRelPath)
	if _, err := os.Stat(bundleSrc); err == nil {
		fmt.Printf("built bundle: %s (present)\n", bundleSrc)
	} else {
		fmt.Printf("built bundle: %s (not built — run: ./bin/usbwifi driver build)\n", bundleSrc)
	}

	bundleDst := filepath.Join(driverExtDir, "AIC8800D80Driver.dext")
	if _, err := os.Stat(bundleDst); err == nil {
		fmt.Printf("installed bundle: %s (present)\n", bundleDst)
	} else {
		fmt.Printf("installed bundle: %s (not installed)\n", bundleDst)
	}

	fmt.Println()
	fmt.Println("systemextensionsctl status:")
	out, err := exec.Command("systemextensionsctl", "list").CombinedOutput()
	if err != nil {
		fmt.Printf("(systemextensionsctl failed: %v)\n", err)
	} else {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(strings.ToLower(line), "aic") || strings.Contains(line, driverBundleID) {
				fmt.Printf("  %s\n", line)
			}
		}
	}
	return 0
}

// findRepoRoot locates the Event Horizon repo root by walking up from
// the current directory until a Makefile is found.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("Makefile not found above %s", cwd)
}

// runCmd runs a command and returns any error (output is unbuffered).
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func usageDriver() {
	fmt.Fprintf(os.Stderr, `driver — manage the AIC8800D80 DriverKit driver

Usage:
  sudo ./bin/usbwifi driver install      # build, install, load
  sudo ./bin/usbwifi driver uninstall    # unload and remove
  ./bin/usbwifi driver status            # show build / install / load status
  ./bin/usbwifi driver build             # build the .dext bundle only

Examples:
  # First-time install (one-shot):
  sudo ./bin/usbwifi driver install

  # Verify load status:
  ./bin/usbwifi driver status

To run the loader (Phase 1) without the driver:
  sudo ./bin/usbwifi aicloader --firmware-dir=<path-to-firmware>
`)
}
