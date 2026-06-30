package executor

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// SetupWSL provisions a dedicated Alpine WSL distro and configures gVisor inside it.
func SetupWSL(dataDir string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("wslsetup is only supported on Windows host environments")
	}

	// 1. Verify wsl.exe is in PATH
	_, err := exec.LookPath("wsl")
	if err != nil {
		return fmt.Errorf("WSL command 'wsl' not found in PATH. Please ensure WSL is installed on your Windows host")
	}

	wslDir := filepath.Join(dataDir, "wsl")
	cacheDir := filepath.Join(dataDir, "cache")
	_ = os.MkdirAll(wslDir, 0755)
	_ = os.MkdirAll(cacheDir, 0755)

	arch := "x86_64"
	gvisorArch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
		gvisorArch = "aarch64"
	}

	// Setup Alpine rootfs download paths
	alpineURL := fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/%s/alpine-minirootfs-3.23.4-%s.tar.gz", arch, arch)
	alpineTar := filepath.Join(cacheDir, fmt.Sprintf("alpine-minirootfs-3.23.4-%s.tar.gz", arch))

	// Setup gVisor download paths
	runscURL := fmt.Sprintf("https://storage.googleapis.com/gvisor/releases/release/latest/%s/runsc", gvisorArch)
	runscBin := filepath.Join(cacheDir, "runsc")

	// 2. Download Alpine rootfs if missing
	if _, err := os.Stat(alpineTar); os.IsNotExist(err) {
		fmt.Printf("📥 Downloading Alpine minirootfs from %s...\n", alpineURL)
		if err := downloadFile(alpineURL, alpineTar); err != nil {
			return fmt.Errorf("failed to download Alpine rootfs: %w", err)
		}
		fmt.Println("✅ Alpine rootfs download complete!")
	}

	// 3. Download gVisor static binary if missing
	if _, err := os.Stat(runscBin); os.IsNotExist(err) {
		fmt.Printf("📥 Downloading gVisor runsc static binary from %s...\n", runscURL)
		if err := downloadFile(runscURL, runscBin); err != nil {
			return fmt.Errorf("failed to download gVisor runsc: %w", err)
		}
		fmt.Println("✅ gVisor runsc download complete!")
	}

	// 4. Check if 'botson-sandbox' WSL distribution is already registered
	distroName := "botson-sandbox"
	fmt.Printf("🔍 Checking if %q WSL distribution is registered...\n", distroName)
	checkCmd := exec.Command("wsl", "-d", distroName, "true")
	isRegistered := (checkCmd.Run() == nil)

	if !isRegistered {
		fmt.Printf("📦 Importing %q WSL distribution...\n", distroName)
		distroPath := filepath.Join(wslDir, distroName)
		_ = os.MkdirAll(distroPath, 0755)

		importCmd := exec.Command("wsl", "--import", distroName, distroPath, alpineTar, "--version", "2")
		var output bytes.Buffer
		importCmd.Stdout = &output
		importCmd.Stderr = &output
		if err := importCmd.Run(); err != nil {
			errStr := cleanUTF16String(output.Bytes())
			if strings.Contains(errStr, "HCS_E_HYPERV_NOT_IN_STALLED") || strings.Contains(errStr, "Virtual Machine Platform") || strings.Contains(errStr, "virtualization") {
				return fmt.Errorf("\n❌ WSL 2 Virtualization is disabled or not installed on your system.\n" +
					"  👉 Please enable the 'Virtual Machine Platform' Windows Optional Component and ensure CPU Virtualization is enabled in your BIOS.\n" +
					"  👉 You can enable the component by running: `wsl.exe --install --no-distribution` in an Administrator PowerShell and restarting your computer.\n" +
					"  (WSL Raw Error: %s)", strings.TrimSpace(errStr))
			}
			return fmt.Errorf("failed to import WSL distribution: %w (WSL error: %s)", err, strings.TrimSpace(errStr))
		}
		fmt.Printf("✅ WSL distribution %q successfully imported!\n", distroName)
	} else {
		fmt.Printf("✅ WSL distribution %q is already registered.\n", distroName)
	}

	// 5. Configure gVisor inside the distro
	fmt.Println("⚙️  Configuring gVisor inside the WSL distribution...")
	
	// Create bin folder inside the distro
	mkdirCmd := exec.Command("wsl", "-d", distroName, "mkdir", "-p", "/usr/local/bin")
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("failed to create directory inside WSL: %w", err)
	}

	// Translate local Windows runsc binary path to WSL path
	wslPathCmd := exec.Command("wsl", "-d", distroName, "wslpath", "-u", filepath.ToSlash(runscBin))
	var wslPathBytes bytes.Buffer
	wslPathCmd.Stdout = &wslPathBytes
	if err := wslPathCmd.Run(); err != nil {
		return fmt.Errorf("failed to translate runsc path using wslpath: %w", err)
	}
	wslRunscPath := strings.TrimSpace(wslPathBytes.String())

	// Copy binary inside WSL
	copyCmd := exec.Command("wsl", "-d", distroName, "cp", wslRunscPath, "/usr/local/bin/runsc")
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy runsc inside WSL: %w", err)
	}

	// Make runsc executable
	chmodCmd := exec.Command("wsl", "-d", distroName, "chmod", "+x", "/usr/local/bin/runsc")
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("failed to make runsc executable: %w", err)
	}

	fmt.Println("🎉 WSL gVisor sandbox setup completed successfully! You can now run isolated sandboxes natively on Windows.")
	return nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad HTTP status: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func cleanUTF16String(b []byte) string {
	var result []byte
	for i := 0; i < len(b); i++ {
		if b[i] != 0 {
			// Strip UTF-16 BOM if present
			if i == 0 && (b[i] == 0xFE || b[i] == 0xFF) {
				continue
			}
			if i == 1 && b[i] == 0xFE && b[0] == 0xFF {
				continue
			}
			result = append(result, b[i])
		}
	}
	return string(result)
}

