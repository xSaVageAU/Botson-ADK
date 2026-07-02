package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	fmt.Println("Starting cross-compilation for Botson Multi-Binary Suite...")

	for _, app := range apps {
		appBuildDir := filepath.Join(buildDir, app.Name)
		if err := os.MkdirAll(appBuildDir, 0755); err != nil {
			fmt.Printf("Failed to create app build directory for %s: %v\n", app.Name, err)
			continue
		}

		fmt.Printf("\n--- Building App: %s ---\n", app.Name)

		for _, t := range targets {
			ext := ""
			if t.OS == "windows" {
				ext = ".exe"
			}

			outputName := fmt.Sprintf("%s-%s-%s%s", app.Name, t.OS, t.Arch, ext)
			outputPath := filepath.Join(appBuildDir, outputName)

			fmt.Printf("Building %s/%s -> %s...\n", t.OS, t.Arch, outputPath)

			cmd := exec.Command("go", "build", "-o", outputPath, app.Path)
			cmd.Env = append(os.Environ(),
				"GOOS="+t.OS,
				"GOARCH="+t.Arch,
				"CGO_ENABLED=0", // Explicitly ensure CGO is disabled for clean pure-Go static binaries
			)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				fmt.Printf("Error building %s for %s/%s: %v\n", app.Name, t.OS, t.Arch, err)
				continue
			}
		}
	}

	fmt.Println("\nBuild process completed! Binaries are organized in the './build/' subfolders.")
}
