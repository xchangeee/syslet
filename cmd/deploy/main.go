// syslet-deploy deploys syslet specs to a remote host via SSH.
//
// Usage:
//
//	syslet-deploy <host> <spec-dir>
//
// Example:
//
//	syslet-deploy web01 hosts/web01/
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Deploy syslet specs to a remote host via SSH.
//
// Responsibilities:
// - Validate command-line arguments (host and spec directory)
// - Package all .json files from the spec directory into a zip archive
// - Transfer the zip to the remote host via SCP
// - Execute syslet on the remote host to apply the configuration
//
// This script orchestrates the deployment pipeline, delegating network operations
// to SSH/SCP and relying on the remote syslet binary for actual system configuration.
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <host> <spec-dir>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample:\n  %s web01 hosts/web01/\n", os.Args[0])
		os.Exit(1)
	}

	host := os.Args[1]
	specDir := os.Args[2]

	// Validate spec directory exists
	info, err := os.Stat(specDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", specDir)
		os.Exit(1)
	}

	// Create temporary zip file
	tmpZip, err := os.CreateTemp("", "syslet-*.zip")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp file: %v\n", err)
		os.Exit(1)
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath) // Clean up on exit

	// Zip all .json files from spec directory (flat, no directory structure)
	if err := zipJSONFiles(tmpZip, specDir); err != nil {
		tmpZip.Close()
		fmt.Fprintf(os.Stderr, "Error creating zip: %v\n", err)
		os.Exit(1)
	}
	tmpZip.Close()

	fmt.Printf("Created zip: %s\n", tmpZipPath)

	// Create remote directory
	if err := runCommand("ssh", host, "mkdir -p /etc/syslet"); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating remote directory: %v\n", err)
		os.Exit(1)
	}

	// Copy zip to remote host
	remotePath := fmt.Sprintf("%s:/etc/syslet/config.zip", host)
	if err := runCommand("scp", tmpZipPath, remotePath); err != nil {
		fmt.Fprintf(os.Stderr, "Error copying to remote host: %v\n", err)
		os.Exit(1)
	}

	// Run syslet on remote host
	if err := runCommand("ssh", host, "syslet /etc/syslet/config.zip"); err != nil {
		fmt.Fprintf(os.Stderr, "Error running syslet: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Deployment successful!")
}

// zipJSONFiles creates a zip archive containing all .json files from the specified directory.
// Files are added flat (no directory structure preserved).
func zipJSONFiles(w io.Writer, dir string) error {
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	// Find all .json files in the directory
	pattern := filepath.Join(dir, "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob pattern error: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Errorf("no .json files found in %s", dir)
	}

	// Add each file to the zip
	for _, path := range matches {
		if err := addFileToZip(zipWriter, path); err != nil {
			return err
		}
	}

	return nil
}

// addFileToZip adds a single file to the zip archive with only its basename (no directory path).
func addFileToZip(zipWriter *zip.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", filePath, err)
	}
	defer file.Close()

	// Use only the basename (flat structure)
	basename := filepath.Base(filePath)
	writer, err := zipWriter.Create(basename)
	if err != nil {
		return fmt.Errorf("creating zip entry for %s: %w", basename, err)
	}

	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("writing %s to zip: %w", basename, err)
	}

	return nil
}

// runCommand executes a command with arguments, streaming output to stdout/stderr.
// It returns an error if the command fails.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
