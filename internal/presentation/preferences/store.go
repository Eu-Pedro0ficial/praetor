package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	fileName       = "presentation-v1.json"
	lockName       = "presentation.lock"
	maximumBytes   = 16 * 1024
	lockWaitPeriod = 5 * time.Second
)

func ResolveConfigDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
		}
		return filepath.Join(filepath.Clean(configured), "praetor"), nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("user configuration directory must be absolute")
	}
	return filepath.Join(filepath.Clean(root), "praetor"), nil
}

func Path(directory string) string { return filepath.Join(directory, fileName) }

func load(directory string) (Layout, error) {
	path := Path(directory)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return Layout{}, fmt.Errorf("inspect presentation preferences: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Layout{}, fmt.Errorf("presentation preferences must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Layout{}, fmt.Errorf("presentation preferences permissions must not permit group or other access")
	}
	if info.Size() > maximumBytes {
		return Layout{}, fmt.Errorf("presentation preferences exceed %d bytes", maximumBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return Layout{}, fmt.Errorf("open presentation preferences: %w", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximumBytes+1))
	if err != nil {
		return Layout{}, fmt.Errorf("read presentation preferences: %w", err)
	}
	if len(payload) > maximumBytes {
		return Layout{}, fmt.Errorf("presentation preferences exceed %d bytes", maximumBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var layout Layout
	if err := decoder.Decode(&layout); err != nil {
		return Layout{}, fmt.Errorf("decode presentation preferences: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Layout{}, err
	}
	if err := layout.Validate(); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

func save(directory string, layout Layout) error {
	if err := layout.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("presentation configuration directory must be absolute")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create presentation configuration directory: %w", err)
	}
	lock, err := acquireLock(directory)
	if err != nil {
		return err
	}
	defer releaseLock(lock)

	path := Path(directory)
	if info, statError := os.Lstat(path); statError == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("presentation preferences must be a regular file")
		}
	} else if !os.IsNotExist(statError) {
		return fmt.Errorf("inspect presentation preferences: %w", statError)
	}
	payload, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return fmt.Errorf("encode presentation preferences: %w", err)
	}
	payload = append(payload, '\n')
	temporary, err := os.CreateTemp(directory, ".presentation-*.tmp")
	if err != nil {
		return fmt.Errorf("create presentation preference temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("set presentation preference permissions: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write presentation preferences: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("sync presentation preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close presentation preferences: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace presentation preferences: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open presentation configuration directory for sync: %w", err)
	}
	syncError := directoryHandle.Sync()
	closeError := directoryHandle.Close()
	if syncError != nil {
		return fmt.Errorf("sync presentation configuration directory: %w", syncError)
	}
	if closeError != nil {
		return fmt.Errorf("close presentation configuration directory: %w", closeError)
	}
	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("presentation preferences contain trailing JSON")
	}
	return fmt.Errorf("decode presentation preferences: %w", err)
}

func acquireLock(directory string) (*os.File, error) {
	path := filepath.Join(directory, lockName)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("presentation preference lock must not be a symbolic link")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect presentation preference lock: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open presentation preference lock: %w", err)
	}
	deadline := time.Now().Add(lockWaitPeriod)
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = file.Close()
			return nil, fmt.Errorf("lock presentation preferences: %w", err)
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("lock presentation preferences: timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func releaseLock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
