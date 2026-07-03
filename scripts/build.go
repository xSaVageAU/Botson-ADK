package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Target struct {
	OS   string
	Arch string
}

type App struct {
	Name string
	Path string
}

func main() {
	targets := []Target{
		{"windows", "amd64"},
		{"windows", "arm64"},
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	}

	apps := []App{
		{"botson", "./apps/botson"},
		{"botson-discord", "./apps/botson-discord"},
		{"botson-web", "./apps/botson-web"},
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
