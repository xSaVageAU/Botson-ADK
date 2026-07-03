package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"flag"
)

type Target struct {
	OS   string
	Arch string
}

type App struct {
	Name string
	Path string
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if strings.TrimSpace(item) == val {
			return true
		}
	}
	return false
}

func main() {
	// 1. Define command line flags for filtering
	osFilter := flag.String("os", "", "Filter targets by OS (comma-separated, e.g. windows,linux). Empty builds all.")
	archFilter := flag.String("arch", "", "Filter targets by architecture (comma-separated, e.g. amd64,arm64). Empty builds all.")
	appFilter := flag.String("app", "", "Filter applications to build (comma-separated, e.g. botson,botson-web). Empty builds all.")
	flag.Parse()

	allTargets := []Target{
		{"windows", "amd64"},
		{"windows", "arm64"},
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	}

	allApps := []App{
		{"botson", "./apps/botson"},
		{"botson-discord", "./apps/botson-discord"},
		{"botson-web", "./apps/botson-web"},
	}

	// 2. Parse and filter targets & apps based on flags
	var selectedOS []string
	if *osFilter != "" {
		selectedOS = strings.Split(*osFilter, ",")
	}
	var selectedArch []string
	if *archFilter != "" {
		selectedArch = strings.Split(*archFilter, ",")
	}
	var selectedApps []string
	if *appFilter != "" {
		selectedApps = strings.Split(*appFilter, ",")
	}

	var targets []Target
	for _, t := range allTargets {
		if len(selectedOS) > 0 && !contains(selectedOS, t.OS) {
			continue
		}
		if len(selectedArch) > 0 && !contains(selectedArch, t.Arch) {
			continue
		}
		targets = append(targets, t)
	}

	var apps []App
	for _, app := range allApps {
		if len(selectedApps) > 0 && !contains(selectedApps, app.Name) {
			continue
		}
		apps = append(apps, app)
	}

	if len(targets) == 0 {
		fmt.Println("No compilation targets matched the filter criteria.")
		os.Exit(0)
	}
	if len(apps) == 0 {
		fmt.Println("No applications matched the filter criteria.")
		os.Exit(0)
	}

	buildDir := "build"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		fmt.Printf("Failed to create build directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting concurrent cross-compilation for Botson Multi-Binary Suite...")

	var wg sync.WaitGroup
	for _, app := range apps {
		wg.Add(1)
		go func(app App) {
			defer wg.Done()

			appBuildDir := filepath.Join(buildDir, app.Name)
			if err := os.MkdirAll(appBuildDir, 0755); err != nil {
				fmt.Printf("Failed to create app build directory for %s: %v\n", app.Name, err)
				return
			}

			var logBuf strings.Builder
			logBuf.WriteString(fmt.Sprintf("\n--- Building App: %s ---\n", app.Name))

			for _, t := range targets {
				ext := ""
				if t.OS == "windows" {
					ext = ".exe"
				}

				outputName := fmt.Sprintf("%s-%s-%s%s", app.Name, t.OS, t.Arch, ext)
				outputPath := filepath.Join(appBuildDir, outputName)

				logBuf.WriteString(fmt.Sprintf("Building %s/%s -> %s...\n", t.OS, t.Arch, outputPath))

				cmd := exec.Command("go", "build", "-o", outputPath, app.Path)
				cmd.Env = append(os.Environ(),
					"GOOS="+t.OS,
					"GOARCH="+t.Arch,
					"CGO_ENABLED=0",
				)

				output, err := cmd.CombinedOutput()
				if err != nil {
					logBuf.WriteString(fmt.Sprintf("Error building %s for %s/%s: %v\nOutput: %s\n", app.Name, t.OS, t.Arch, err, string(output)))
					continue
				}
			}

			fmt.Print(logBuf.String())
		}(app)
	}

	wg.Wait()
	fmt.Println("\nBuild process completed! Binaries are organized in the './build/' subfolders.")
}
