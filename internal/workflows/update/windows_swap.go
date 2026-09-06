package update

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

const (
	windowsSwapWaitTimeout   = 2 * time.Minute
	windowsSwapRetryInterval = 1 * time.Second
	windowsSwapRetryTimeout  = 2 * time.Minute
)

type windowsSwapLaunchOptions struct {
	ParentPID     int
	TargetPath    string
	StagedPath    string
	ResultPath    string
	WaitTimeout   time.Duration
	RetryInterval time.Duration
	RetryTimeout  time.Duration
}

type windowsSwapOutcome struct {
	Applied  bool
	Attempts int
}

type windowsSwapRuntime struct {
	remove     func(string) error
	rename     func(string, string) error
	pathExists func(string) bool
	sleep      func(time.Duration)
	now        func() time.Time
}

func launchDetachedWindowsSwap(ctx context.Context, options windowsSwapLaunchOptions) error {
	command, err := buildWindowsSwapCommand(ctx, options)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to start Windows update worker", err)
	}
	if command.Process != nil {
		_ = command.Process.Release()
	}
	return nil
}

func buildWindowsSwapCommand(ctx context.Context, options windowsSwapLaunchOptions) (*exec.Cmd, error) {
	_ = ctx

	script, err := buildWindowsSwapScript(options)
	if err != nil {
		return nil, err
	}
	encoded := encodePowerShellCommand(script)
	// #nosec G204 -- a fixed binary with fixed flags. The script is built here
	// and base64-encoded, which is what -EncodedCommand exists for: it removes
	// the quoting layer where an injected path could otherwise become a
	// statement.
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded), nil
}

func buildWindowsSwapScript(options windowsSwapLaunchOptions) (string, error) {
	targetPath := strings.TrimSpace(options.TargetPath)
	stagedPath := strings.TrimSpace(options.StagedPath)
	resultPath := strings.TrimSpace(options.ResultPath)
	if targetPath == "" || stagedPath == "" || resultPath == "" {
		return "", apperrors.New(apperrors.KindValidation, "windows swap target, staged, and result paths are required", nil)
	}

	backupPath := windowsSwapBackupPath(targetPath)
	waitSeconds := durationSecondsOrDefault(options.WaitTimeout, windowsSwapWaitTimeout)
	retrySeconds := durationSecondsOrDefault(options.RetryTimeout, windowsSwapRetryTimeout)
	retryMilliseconds := durationMillisecondsOrDefault(options.RetryInterval, windowsSwapRetryInterval)

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$parentPid = %d
$targetPath = '%s'
$stagedPath = '%s'
$backupPath = '%s'
$resultPath = '%s'
$waitSeconds = %d
$retrySeconds = %d
$retryIntervalMilliseconds = %d
$status = 'failed'
$lastError = ''
$attempt = 0

if (Test-Path -LiteralPath $resultPath) {
    Remove-Item -LiteralPath $resultPath -Force -ErrorAction SilentlyContinue
}

try {
    if ($parentPid -gt 0) {
        try {
            Wait-Process -Id $parentPid -Timeout $waitSeconds -ErrorAction SilentlyContinue
        } catch {
        }
    }

    $deadline = (Get-Date).AddSeconds($retrySeconds)
    while ((Get-Date) -lt $deadline) {
        $attempt += 1
        try {
            if (Test-Path -LiteralPath $backupPath) {
                Remove-Item -LiteralPath $backupPath -Force -ErrorAction SilentlyContinue
            }
            if (Test-Path -LiteralPath $targetPath) {
                Move-Item -LiteralPath $targetPath -Destination $backupPath -Force
            }
            Move-Item -LiteralPath $stagedPath -Destination $targetPath -Force
            if (Test-Path -LiteralPath $backupPath) {
                Remove-Item -LiteralPath $backupPath -Force -ErrorAction SilentlyContinue
            }
            $status = 'applied'
            $lastError = ''
            break
        } catch {
            $lastError = $_.Exception.Message
            if ((Test-Path -LiteralPath $backupPath) -and -not (Test-Path -LiteralPath $targetPath)) {
                try {
                    Move-Item -LiteralPath $backupPath -Destination $targetPath -Force
                } catch {
                    $lastError = $_.Exception.Message
                }
            }
            Start-Sleep -Milliseconds $retryIntervalMilliseconds
        }
    }

    if ($status -ne 'applied') {
        $status = 'failed'
    }
} catch {
    $status = 'failed'
    $lastError = $_.Exception.Message
}

[ordered]@{
    status = $status
    target_path = $targetPath
    staged_path = $stagedPath
    attempts = $attempt
    error = $lastError
} | ConvertTo-Json -Compress | Set-Content -LiteralPath $resultPath -Encoding UTF8
`, options.ParentPID, escapePowerShellLiteral(targetPath), escapePowerShellLiteral(stagedPath), escapePowerShellLiteral(backupPath), escapePowerShellLiteral(resultPath), waitSeconds, retrySeconds, retryMilliseconds)

	return script, nil
}

func durationSecondsOrDefault(value, fallback time.Duration) int {
	if value <= 0 {
		value = fallback
	}
	return int(value / time.Second)
}

func durationMillisecondsOrDefault(value, fallback time.Duration) int {
	if value <= 0 {
		value = fallback
	}
	return int(value / time.Millisecond)
}

func encodePowerShellCommand(script string) string {
	utf16Words := utf16.Encode([]rune(script))
	bytes := make([]byte, 0, len(utf16Words)*2)
	for _, word := range utf16Words {
		// Masked rather than truncated by accident: this is a little-endian
		// UTF-16 split, so losing the high bits of the low byte is the point.
		bytes = append(bytes, byte(word&0xFF), byte(word>>8))
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func escapePowerShellLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func windowsSwapResultPath(targetPath string) string {
	return strings.TrimSpace(targetPath) + ".update-result.json"
}

func windowsSwapBackupPath(targetPath string) string {
	return strings.TrimSpace(targetPath) + ".bak"
}

func executeWindowsSwap(options windowsSwapLaunchOptions, runtime windowsSwapRuntime) (windowsSwapOutcome, error) {
	targetPath := strings.TrimSpace(options.TargetPath)
	stagedPath := strings.TrimSpace(options.StagedPath)
	if targetPath == "" || stagedPath == "" {
		return windowsSwapOutcome{}, apperrors.New(apperrors.KindValidation, "windows swap target and staged paths are required", nil)
	}

	remove := runtime.remove
	if remove == nil {
		remove = os.Remove
	}
	rename := runtime.rename
	if rename == nil {
		rename = os.Rename
	}
	pathExists := runtime.pathExists
	if pathExists == nil {
		pathExists = func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}
	}
	sleep := runtime.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	now := runtime.now
	if now == nil {
		now = time.Now
	}

	backupPath := windowsSwapBackupPath(targetPath)
	retryInterval := options.RetryInterval
	if retryInterval <= 0 {
		retryInterval = windowsSwapRetryInterval
	}
	retryTimeout := options.RetryTimeout
	if retryTimeout <= 0 {
		retryTimeout = windowsSwapRetryTimeout
	}
	deadline := now().Add(retryTimeout)
	attempts := 0
	var lastErr error

	for attempts == 0 || now().Before(deadline) {
		attempts++
		_ = remove(backupPath)
		if pathExists(targetPath) {
			if err := rename(targetPath, backupPath); err != nil {
				lastErr = err
				sleep(retryInterval)
				continue
			}
		}
		if err := rename(stagedPath, targetPath); err != nil {
			lastErr = err
			if pathExists(backupPath) && !pathExists(targetPath) {
				_ = rename(backupPath, targetPath)
			}
			sleep(retryInterval)
			continue
		}
		_ = remove(backupPath)
		return windowsSwapOutcome{Applied: true, Attempts: attempts}, nil
	}

	if lastErr == nil {
		lastErr = apperrors.New(apperrors.KindInternal, "windows swap retry deadline exceeded", nil)
	}
	return windowsSwapOutcome{Applied: false, Attempts: attempts}, apperrors.New(apperrors.KindInternal, "failed to replace Windows binary after exit", lastErr)
}
