package sandbox

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type RootfsManager struct {
	CacheDir string
}

func NewRootfsManager(cacheDir string) *RootfsManager {
	return &RootfsManager{
		CacheDir: cacheDir,
	}
}

// EnsureTemplateRootfs guarantees that a template rootfs is downloaded and unpacked in the cache directory
func (rm *RootfsManager) EnsureTemplateRootfs() (string, error) {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}

	alpineURL := fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/%s/alpine-minirootfs-3.23.4-%s.tar.gz", arch, arch)
	templateDir := filepath.Join(rm.CacheDir, "alpine-template-3.23.4-"+arch)
	tarPath := filepath.Join(rm.CacheDir, "alpine-minirootfs-3.23.4-"+arch+".tar.gz")

	// If the template directory already has a /bin directory, we assume it's good
	if _, err := os.Stat(filepath.Join(templateDir, "bin")); err == nil {
		return templateDir, nil
	}

	// Make sure cache dir exists
	if err := os.MkdirAll(rm.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Download the minirootfs tarball
	if _, err := os.Stat(tarPath); os.IsNotExist(err) {
		fmt.Printf("📥 Downloading Alpine minirootfs from %s...\n", alpineURL)
		if err := rm.downloadFile(tarPath, alpineURL); err != nil {
			return "", fmt.Errorf("failed to download Alpine rootfs: %w", err)
		}
		fmt.Println("✅ Download complete!")
	}

	// Unpack the minirootfs tarball
	fmt.Printf("📦 Unpacking rootfs to %s...\n", templateDir)
	if err := os.MkdirAll(templateDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create template directory: %w", err)
	}

	if err := rm.unpackTarGz(tarPath, templateDir); err != nil {
		// Clean up broken template dir on error
		os.RemoveAll(templateDir)
		return "", fmt.Errorf("failed to unpack rootfs: %w", err)
	}
	fmt.Println("✅ Unpacking complete!")

	return templateDir, nil
}

// CopyTemplateTo copies the template rootfs to the target directory recursively
func (rm *RootfsManager) CopyTemplateTo(destDir string) error {
	templateDir, err := rm.EnsureTemplateRootfs()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create target rootfs directory: %w", err)
	}

	return rm.copyDirRecursive(templateDir, destDir)
}

func (rm *RootfsManager) downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad HTTP status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (rm *RootfsManager) unpackTarGz(tarPath string, destDir string) error {
	if runtime.GOOS == "windows" {
		wslTar, err := translateToWSLPath(tarPath)
		if err != nil {
			return fmt.Errorf("translating tar path to WSL: %w", err)
		}
		wslDest, err := translateToWSLPath(destDir)
		if err != nil {
			return fmt.Errorf("translating dest path to WSL: %w", err)
		}

		// Run tar extraction inside WSL to properly handle symlinks on NTFS mounts
		cmd := exec.Command("wsl", "-d", WSLDistro, "tar", "-xzf", wslTar, "-C", wslDest)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("WSL tar extraction failed: %w (output: %s)", err, string(out))
		}
		return nil
	}

	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Avoid zip-slip vulnerability path traversal
		cleanPath := filepath.Clean(header.Name)
		target := filepath.Join(destDir, cleanPath)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent dir exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()

		case tar.TypeSymlink:
			// Ensure parent dir exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}

			// We need to create a symlink. For Windows hosts, symlink creation might fail if not admin.
			// However, since this runs in WSL2/Linux context mostly, this is perfectly fine.
			// If on Windows, we'll attempt it anyway.
			if err := os.Symlink(header.Linkname, target); err != nil {
				// Log warning but continue if symlink fails (useful if running in non-admin on Windows host directly)
				fmt.Printf("⚠️ Warning: failed to create symlink %s -> %s: %v\n", target, header.Linkname, err)
			}
		}
	}

	return nil
}

// copyDirRecursive recursively copies a directory tree, preserving files, directories, modes, and symlinks.
func (rm *RootfsManager) copyDirRecursive(src, dst string) error {
	if runtime.GOOS == "windows" {
		wslSrc, err := translateToWSLPath(src)
		if err != nil {
			return fmt.Errorf("translating source path to WSL: %w", err)
		}
		wslDst, err := translateToWSLPath(dst)
		if err != nil {
			return fmt.Errorf("translating dest path to WSL: %w", err)
		}

		// Run cp inside WSL to preserve symlinks and avoid Windows symlink creation permissions issues.
		// Note that on NTFS mounts, cp -a will print ownership warnings, but it copies all files and symlinks correctly.
		cmd := exec.Command("wsl", "-d", WSLDistro, "cp", "-a", wslSrc+"/.", wslDst+"/")
		if out, err := cmd.CombinedOutput(); err != nil {
			// Check if copying actually succeeded by verifying the destination isn't empty
			if _, statErr := os.Stat(filepath.Join(dst, "bin")); statErr != nil {
				return fmt.Errorf("WSL copy failed: %w (output: %s)", err, string(out))
			}
		}
		return nil
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return err
			}
			if err := rm.copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(linkTarget, dstPath); err != nil {
				return err
			}
		} else {
			if err := rm.copyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (rm *RootfsManager) copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	return nil
}

// SaveAsTemplate saves a copy of an active sandbox's rootfs as a custom template in the cache directory.
func (rm *RootfsManager) SaveAsTemplate(srcDir, templateName string, overwrite bool) error {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}

	templateDir := filepath.Join(rm.CacheDir, "template-"+templateName+"-"+arch)

	if _, err := os.Stat(templateDir); err == nil {
		if !overwrite {
			return fmt.Errorf("a template named '%s' already exists. To overwrite it, pass overwrite=true", templateName)
		}
		// Clean up the existing template directory
		if err := os.RemoveAll(templateDir); err != nil {
			return fmt.Errorf("failed to remove existing template directory: %w", err)
		}
	}

	if err := os.MkdirAll(templateDir, 0755); err != nil {
		return fmt.Errorf("failed to create template directory: %w", err)
	}

	return rm.copyDirRecursive(srcDir, templateDir)
}

// CopyCustomTemplateTo copies a saved custom template from the cache to a target sandbox rootfs destination directory.
func (rm *RootfsManager) CopyCustomTemplateTo(templateName string, destDir string) error {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}

	templateDir := filepath.Join(rm.CacheDir, "template-"+templateName+"-"+arch)
	if _, err := os.Stat(filepath.Join(templateDir, "bin")); os.IsNotExist(err) {
		return fmt.Errorf("custom template '%s' not found or incomplete in cache", templateName)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create target rootfs directory: %w", err)
	}

	return rm.copyDirRecursive(templateDir, destDir)
}

// ListCustomTemplates scans the cache directory and returns the names of all custom templates
func (rm *RootfsManager) ListCustomTemplates() ([]string, error) {
	entries, err := os.ReadDir(rm.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	prefix := "template-"
	suffix := "-" + arch

	var templates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			tName := strings.TrimPrefix(name, prefix)
			tName = strings.TrimSuffix(tName, suffix)
			templates = append(templates, tName)
		}
	}
	return templates, nil
}


