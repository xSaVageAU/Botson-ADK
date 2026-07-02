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

func main() {
	targets := []Target{
		{"windows", "amd64"},
		{"windows", "arm64"},
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	}

	buildDir := "build"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		fmt.Printf("Failed to create build directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting cross-compilation for Botson...")

	for _, t := range targets {
		ext := ""
		if t.OS == "windows" {
			ext = ".exe"
		}

		outputName := fmt.Sprintf("botson-%s-%s%s", t.OS, t.Arch, ext)
		outputPath := filepath.Join(buildDir, outputName)

		fmt.Printf("Building %s/%s -> %s...\n", t.OS, t.Arch, outputPath)

		cmd := exec.Command("go", "build", "-o", outputPath, "./apps/botson")
		cmd.Env = append(os.Environ(),
			"GOOS="+t.OS,
			"GOARCH="+t.Arch,
			"CGO_ENABLED=0", // Explicitly ensure CGO is disabled for clean pure-Go static binaries
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Printf("Error building %s/%s: %v\n", t.OS, t.Arch, err)
			continue
		}
	}

	fmt.Println("\nBuild process completed! Binaries are located in the './build/' folder.")
}
