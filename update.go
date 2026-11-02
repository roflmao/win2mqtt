package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const updateRepo = "roflmao/win2mqtt"

// GitHubRelease is the subset of the GitHub release API we read.
type GitHubRelease struct {
	TagName    string        `json:"tag_name"`
	Name       string        `json:"name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []GitHubAsset `json:"assets"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func checkForUpdate() (*GitHubRelease, error) {
	apiURL := "https://api.github.com/repos/" + updateRepo + "/releases/latest"

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "win2mqtt/"+version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release info: %w", err)
	}
	return &release, nil
}

// parseVersion parses "v1.2.3" or "1.2.3" into its numeric parts.
func parseVersion(v string) ([3]int, error) {
	var parts [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	fields := strings.Split(v, ".")
	if len(fields) != 3 {
		return parts, fmt.Errorf("invalid version %q: expected MAJOR.MINOR.PATCH", v)
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return parts, fmt.Errorf("invalid version %q: component %q is not a number", v, f)
		}
		parts[i] = n
	}
	return parts, nil
}

// isNewerVersion reports whether newVer is newer than currentVer, comparing numerically.
func isNewerVersion(currentVer, newVer string) bool {
	if strings.TrimPrefix(currentVer, "v") == "dev" {
		return true
	}
	current, err := parseVersion(currentVer)
	if err != nil {
		log.Printf("Warning: cannot parse current version: %v", err)
		return false
	}
	latest, err := parseVersion(newVer)
	if err != nil {
		log.Printf("Warning: cannot parse release version: %v", err)
		return false
	}
	for i := 0; i < 3; i++ {
		if latest[i] != current[i] {
			return latest[i] > current[i]
		}
	}
	return false
}

func getBinaryNameForPlatform() string {
	return fmt.Sprintf("win2mqtt-windows-%s.exe", nativeArch())
}

func findAssetForCurrentPlatform(release *GitHubRelease) (*GitHubAsset, error) {
	expected := getBinaryNameForPlatform()
	for i := range release.Assets {
		if release.Assets[i].Name == expected {
			return &release.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("no binary found for platform %s/%s (looking for %s)", runtime.GOOS, runtime.GOARCH, expected)
}

// downloadBinary fetches the asset into a temp file next to the running executable
// so the final rename stays on one volume. Returns the temp path.
func downloadBinary(asset *GitHubAsset, dir string) (string, error) {
	log.Printf("Downloading update from %s (%d bytes)", asset.BrowserDownloadURL, asset.Size)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(asset.BrowserDownloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	if resp.ContentLength > 0 && resp.ContentLength != asset.Size {
		return "", fmt.Errorf("size mismatch: expected %d bytes, got %d", asset.Size, resp.ContentLength)
	}

	tmpFile, err := os.CreateTemp(dir, "win2mqtt-update-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	written, err := io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write binary: %w", err)
	}
	if written != asset.Size {
		os.Remove(tmpPath)
		return "", fmt.Errorf("incomplete download: expected %d bytes, got %d", asset.Size, written)
	}

	log.Printf("Downloaded %d bytes to %s", written, tmpPath)
	return tmpPath, nil
}

func getCurrentExecutablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlinks: %w", err)
	}
	return exePath, nil
}

func oldBinaryPath(exePath string) string { return exePath + ".old" }

// replaceBinary swaps the running executable for newBinaryPath. Windows allows
// renaming a running exe, so the old one is moved aside rather than deleted.
func replaceBinary(newBinaryPath string) error {
	currentPath, err := getCurrentExecutablePath()
	if err != nil {
		return err
	}
	backupPath := oldBinaryPath(currentPath)
	log.Printf("Replacing binary at %s", currentPath)

	os.Remove(backupPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to move current binary aside: %w", err)
	}
	if err := os.Rename(newBinaryPath, currentPath); err != nil {
		log.Printf("Failed to move new binary, restoring backup: %v", err)
		if restoreErr := os.Rename(backupPath, currentPath); restoreErr != nil {
			return fmt.Errorf("failed to install new binary and restore backup: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	log.Printf("Binary replaced successfully, previous version kept at %s", backupPath)
	return nil
}

// cleanupOldBinary removes the previous version left behind by an update.
func cleanupOldBinary() {
	exePath, err := getCurrentExecutablePath()
	if err != nil {
		return
	}
	if err := os.Remove(oldBinaryPath(exePath)); err == nil {
		log.Printf("Removed previous binary %s", oldBinaryPath(exePath))
	}
}

func performUpdate() error {
	log.Printf("Current version: %s", version)

	release, err := checkForUpdate()
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	if release.Draft || release.Prerelease {
		log.Printf("Skipping %s (draft=%v, prerelease=%v)", release.TagName, release.Draft, release.Prerelease)
		return nil
	}
	if !isNewerVersion(version, release.TagName) {
		log.Printf("Already running latest version (current: %s, latest: %s)", version, release.TagName)
		return nil
	}
	log.Printf("New version available: %s -> %s", version, release.TagName)

	asset, err := findAssetForCurrentPlatform(release)
	if err != nil {
		return fmt.Errorf("update aborted: %w", err)
	}

	exePath, err := getCurrentExecutablePath()
	if err != nil {
		return err
	}
	tmpPath, err := downloadBinary(asset, filepath.Dir(exePath))
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpPath)

	if err := replaceBinary(tmpPath); err != nil {
		return fmt.Errorf("binary replacement failed: %w", err)
	}
	log.Printf("Update to %s completed successfully", release.TagName)

	if finishUpdate(relaunch) {
		log.Println("Exiting so the new version can start")
		os.Exit(0)
	}
	return nil
}

// restartPending is set when a new binary is installed but nothing could be
// arranged to restart us; further update checks are pointless until then.
var restartPending atomic.Bool

// finishUpdate schedules the relaunch and reports whether this process should
// exit. If no relaunch could be scheduled the current process keeps running,
// since exiting would leave nothing behind until the next logon.
func finishUpdate(relaunch func() error) bool {
	if err := relaunch(); err != nil {
		log.Printf("Warning: could not schedule relaunch: %v. Staying up; the new version starts at the next restart", err)
		restartPending.Store(true)
		return false
	}
	return true
}

// checkAndApplyUpdate is the entry point for scheduled update checks.
func checkAndApplyUpdate() {
	if restartPending.Load() {
		log.Println("Update already installed, waiting for restart")
		return
	}
	log.Println("Checking for updates...")
	if err := performUpdate(); err != nil {
		log.Printf("Update failed: %v (will retry in 1 hour)", err)
		return
	}
	log.Println("No update required")
}
