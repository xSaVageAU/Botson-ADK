package executor

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type SetupLogger struct {
	mu   sync.Mutex
	logs []string
}

var GlobalSetupLogger SetupLogger

func (l *SetupLogger) Log(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.logs = append(l.logs, msg)
	l.mu.Unlock()
	log.Println(msg)
}

func (l *SetupLogger) GetLogs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	res := make([]string, len(l.logs))
	copy(res, l.logs)
	return res
}

func (l *SetupLogger) Clear() {
	l.mu.Lock()
	l.logs = []string{}
	l.mu.Unlock()
}

type progressWriter struct {
	total      int64
	downloaded int64
	onProgress func(downloaded, total int64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.downloaded += int64(n)
	if pw.onProgress != nil {
		pw.onProgress(pw.downloaded, pw.total)
	}
	return n, nil
}

// SetupSandbox automatically determines the platform and configures the sandbox
func SetupSandbox(dataDir string) error {
	if runtime.GOOS == "windows" {
		return SetupWSL(dataDir)
	}
	return SetupLinux(dataDir)
}

// IsSandboxSetup checks if sandbox execution is provisioned and ready on the system
func IsSandboxSetup(dataDir string) bool {
	if runtime.GOOS == "windows" {
		_, err := exec.LookPath("wsl")
		if err != nil {
			return false
		}
		cmd := exec.Command("wsl", "-d", "botson-sandbox", "runsc", "--version")
		return cmd.Run() == nil
	} else {
		_, errPath := exec.LookPath("runsc")
		if errPath == nil {
			return true
		}
		localRunsc := filepath.Join(dataDir, "cache", "runsc")
		info, err := os.Stat(localRunsc)
		if err != nil {
			return false
		}
		return info.Mode()&0111 != 0
	}
}

// SetupLinux downloads and configures gVisor runsc locally on Linux hosts
func SetupLinux(dataDir string) error {
	cacheDir := filepath.Join(dataDir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)

	gvisorArch := "x86_64"
	if runtime.GOARCH == "arm64" {
		gvisorArch = "aarch64"
	}

	runscURL := fmt.Sprintf("https://storage.googleapis.com/gvisor/releases/release/latest/%s/runsc", gvisorArch)
	runscBin := filepath.Join(cacheDir, "runsc")

	// Download runsc if missing
	if _, err := os.Stat(runscBin); os.IsNotExist(err) {
		GlobalSetupLogger.Log("📥 Downloading gVisor runsc static binary...")
		if err := downloadFile(runscURL, runscBin, true); err != nil {
			return fmt.Errorf("failed to download gVisor runsc: %w", err)
		}
		GlobalSetupLogger.Log("✅ gVisor runsc download complete!")
	} else {
		GlobalSetupLogger.Log("✅ gVisor runsc static binary is already downloaded.")
	}

	// Make runsc executable
	if err := os.Chmod(runscBin, 0755); err != nil {
		return fmt.Errorf("failed to make runsc executable: %w", err)
	}
	GlobalSetupLogger.Log("🎉 Local Linux gVisor sandbox setup completed successfully!")
	return nil
}

// SetupWSL provisions a dedicated Alpine WSL distro and configures gVisor inside it.
func SetupWSL(dataDir string) error {
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
		GlobalSetupLogger.Log("📥 Downloading Alpine minirootfs...")
		if err := downloadFile(alpineURL, alpineTar, true); err != nil {
			return fmt.Errorf("failed to download Alpine rootfs: %w", err)
		}
		GlobalSetupLogger.Log("✅ Alpine rootfs download complete!")
	} else {
		GlobalSetupLogger.Log("✅ Alpine rootfs tarball already cached.")
	}

	// 3. Download gVisor static binary if missing
	if _, err := os.Stat(runscBin); os.IsNotExist(err) {
		GlobalSetupLogger.Log("📥 Downloading gVisor runsc static binary...")
		if err := downloadFile(runscURL, runscBin, true); err != nil {
			return fmt.Errorf("failed to download gVisor runsc: %w", err)
		}
		GlobalSetupLogger.Log("✅ gVisor runsc download complete!")
	} else {
		GlobalSetupLogger.Log("✅ gVisor runsc static binary already cached.")
	}

	// 4. Check if 'botson-sandbox' WSL distribution is already registered
	distroName := "botson-sandbox"
	GlobalSetupLogger.Log("🔍 Checking if %q WSL distribution is registered...", distroName)
	checkCmd := exec.Command("wsl", "-d", distroName, "true")
	isRegistered := (checkCmd.Run() == nil)

	if !isRegistered {
		GlobalSetupLogger.Log("📦 Importing %q WSL distribution...", distroName)
		distroPath := filepath.Join(wslDir, distroName)
		_ = os.MkdirAll(distroPath, 0755)

		importCmd := exec.Command("wsl", "--import", distroName, distroPath, alpineTar, "--version", "2")
		var output bytes.Buffer
		importCmd.Stdout = &output
		importCmd.Stderr = &output
		if err := importCmd.Run(); err != nil {
			errStr := cleanUTF16String(output.Bytes())
			if strings.Contains(errStr, "HCS_E_HYPERV_NOT_IN_STALLED") || strings.Contains(errStr, "Virtual Machine Platform") || strings.Contains(errStr, "virtualization") {
				return fmt.Errorf("\n❌ WSL 2 Virtualization is disabled or not installed on your system.\n"+
					"  👉 Please enable the 'Virtual Machine Platform' Windows Optional Component and ensure CPU Virtualization is enabled in your BIOS.\n"+
					"  👉 You can enable the component by running: `wsl.exe --install --no-distribution` in an Administrator PowerShell and restarting your computer.\n"+
					"  (WSL Raw Error: %s)", strings.TrimSpace(errStr))
			}
			return fmt.Errorf("failed to import WSL distribution: %w (WSL error: %s)", err, strings.TrimSpace(errStr))
		}
		GlobalSetupLogger.Log("✅ WSL distribution %q successfully imported!", distroName)
	} else {
		GlobalSetupLogger.Log("✅ WSL distribution %q is already registered.", distroName)
	}

	// 5. Configure gVisor inside the distro
	GlobalSetupLogger.Log("⚙️  Configuring gVisor inside the WSL distribution...")

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

	GlobalSetupLogger.Log("🎉 WSL gVisor sandbox setup completed successfully!")
	return nil
}

func downloadFile(url, dest string, logMilestones bool) error {
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

	var lastLoggedPercent int = -1
	pw := &progressWriter{
		total: resp.ContentLength,
		onProgress: func(downloaded, total int64) {
			if total > 0 {
				percent := float64(downloaded) / float64(total) * 100
				bars := int(percent / 5)
				barStr := strings.Repeat("█", bars) + strings.Repeat("░", 20-bars)
				fmt.Printf("\r   [%s] %.1f%% (%d/%d MB)", barStr, percent, downloaded/(1024*1024), total/(1024*1024))

				if logMilestones {
					intPercent := int(percent)
					if intPercent%10 == 0 && intPercent != lastLoggedPercent {
						lastLoggedPercent = intPercent
						GlobalSetupLogger.Log("Downloading... %d%% (%d/%d MB)", intPercent, downloaded/(1024*1024), total/(1024*1024))
					}
				}
			} else {
				fmt.Printf("\r   Downloaded %d KB...", downloaded/1024)
			}
		},
	}

	_, err = io.Copy(io.MultiWriter(out, pw), resp.Body)
	fmt.Println()
	return err
}

func cleanUTF16String(b []byte) string {
	var result []byte
	for i := 0; i < len(b); i++ {
		if b[i] != 0 {
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
