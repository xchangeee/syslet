// syslet-push deploys syslet specs to a remote host via SSH.
//
// Usage:
//
//	syslet-push [--directory <dir> | --stdin] <host>
//
// Examples:
//
//	syslet-push --directory hosts/web01/ web01
//	cat specs.json | syslet-push --stdin web01
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Deploy syslet specs to a remote host via SSH.
//
// Responsibilities:
// - Validate command-line arguments (host and spec source)
// - Package all specs (from directory or stdin) into a zip archive
// - Transfer the zip to the remote host via SCP
// - Execute syslet on the remote host to apply the configuration
//
// This script orchestrates the deployment pipeline, delegating network operations
// to SSH/SCP and relying on the remote syslet binary for actual system configuration.
func main() {
	// Define flags
	dirFlag := flag.String("directory", "", "Directory containing .json spec files")
	stdinFlag := flag.Bool("stdin", false, "Read spec objects from stdin (JSON array or newline-delimited JSON)")
	diffFlag := flag.Bool("diff", false, "Show what would change without applying (dry-run)")
	flag.Parse()

	// Validate flags are mutually exclusive
	if *dirFlag != "" && *stdinFlag {
		fmt.Fprintf(os.Stderr, "Error: --directory and --stdin are mutually exclusive\n")
		flag.Usage()
		os.Exit(1)
	}

	if *dirFlag == "" && !*stdinFlag {
		fmt.Fprintf(os.Stderr, "Error: must specify either --directory or --stdin\n")
		flag.Usage()
		os.Exit(1)
	}

	// Get host from remaining arguments
	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [--directory <dir> | --stdin] <host>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s --directory hosts/web01/ web01\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  cat specs.json | %s --stdin web01\n", os.Args[0])
		os.Exit(1)
	}
	host := flag.Arg(0)

	// Create temporary zip file
	tmpZip, err := os.CreateTemp("", "syslet-*.zip")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp file: %v\n", err)
		os.Exit(1)
	}
	tmpZipPath := tmpZip.Name()
	defer func() { _ = os.Remove(tmpZipPath) }() // Clean up on exit

	// Zip specs based on source
	if *stdinFlag {
		// Read from stdin
		if err := zipFromStdin(tmpZip); err != nil {
			_ = tmpZip.Close()
			fmt.Fprintf(os.Stderr, "Error creating zip from stdin: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Read from directory
		specDir := *dirFlag

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

		if err := zipJSONFiles(tmpZip, specDir); err != nil {
			_ = tmpZip.Close()
			fmt.Fprintf(os.Stderr, "Error creating zip: %v\n", err)
			os.Exit(1)
		}
	}
	if err := tmpZip.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing zip file: %v\n", err)
		os.Exit(1)
	}

	// Create remote directory
	if err := runCommand("ssh", host, "mkdir -p /etc/syslet"); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating remote directory: %v\n", err)
		os.Exit(1)
	}

	// Use different remote file for diff vs apply
	var remoteFile string
	if *diffFlag {
		remoteFile = "/etc/syslet/preview.zip"
	} else {
		remoteFile = "/etc/syslet/config.zip"
	}

	// Copy zip to remote host
	remotePath := fmt.Sprintf("%s:%s", host, remoteFile)
	if err := runCommand("scp", tmpZipPath, remotePath); err != nil {
		fmt.Fprintf(os.Stderr, "Error copying to remote host: %v\n", err)
		os.Exit(1)
	}

	// Run syslet on remote host
	var sysletCmd string
	if *diffFlag {
		sysletCmd = fmt.Sprintf("syslet --diff %s", remoteFile)
	} else {
		sysletCmd = fmt.Sprintf("syslet %s", remoteFile)
	}
	if err := runCommand("ssh", host, sysletCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error running syslet: %v\n", err)
		os.Exit(1)
	}

	if !*diffFlag {
		fmt.Println("Deployment successful!")
	}
}

// zipFromStdin creates a zip archive containing spec objects read from stdin.
// Supports both JSON array format and newline-delimited JSON.
// Each spec object is written as a separate .json file in the zip.
func zipFromStdin(w io.Writer) error {
	zipWriter := zip.NewWriter(w)
	defer func() { _ = zipWriter.Close() }()

	// Read all stdin content
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	if len(stdinData) == 0 {
		return fmt.Errorf("no data received from stdin")
	}

	// Try to parse as JSON array first
	var specs []json.RawMessage
	if err := json.Unmarshal(stdinData, &specs); err == nil {
		// Successfully parsed as array
		if len(specs) == 0 {
			return fmt.Errorf("empty spec array received from stdin")
		}

		for i, spec := range specs {
			filename := fmt.Sprintf("spec-%03d.json", i)
			if err := addJSONToZip(zipWriter, filename, spec); err != nil {
				return err
			}
		}
		return nil
	}

	// Try newline-delimited JSON
	scanner := bufio.NewScanner(bufio.NewReader(bytes.NewReader(stdinData)))
	lineNum := 0
	specCount := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		// Skip empty lines
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		// Validate it's valid JSON
		var spec json.RawMessage
		if err := json.Unmarshal(line, &spec); err != nil {
			return fmt.Errorf("invalid JSON on line %d: %w", lineNum, err)
		}

		filename := fmt.Sprintf("spec-%03d.json", specCount)
		if err := addJSONToZip(zipWriter, filename, spec); err != nil {
			return err
		}
		specCount++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	if specCount == 0 {
		return fmt.Errorf("no valid spec objects found in stdin")
	}

	return nil
}

// zipJSONFiles creates a zip archive containing all .json files from the specified directory.
// Files are added flat (no directory structure preserved).
func zipJSONFiles(w io.Writer, dir string) error {
	zipWriter := zip.NewWriter(w)
	defer func() { _ = zipWriter.Close() }()

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
	defer func() { _ = file.Close() }()

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

// addJSONToZip adds a JSON object to the zip archive with the specified filename.
func addJSONToZip(zipWriter *zip.Writer, filename string, data json.RawMessage) error {
	writer, err := zipWriter.Create(filename)
	if err != nil {
		return fmt.Errorf("creating zip entry for %s: %w", filename, err)
	}

	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("writing %s to zip: %w", filename, err)
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
