package realm

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/giacomoguidotto/userland/internal/csvfile"
)

var projectionHeader = []string{"source", "target", "mode"}

type fileProjection struct {
	source string
	target string
	mode   os.FileMode
}

func (m Manager) reconcileFileProjections(configurationRoot, mount string, apply bool) ([]Finding, error) {
	projections, err := loadFileProjections(configurationRoot, mount)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, projection := range projections {
		sourceInfo, statErr := os.Stat(projection.source)
		if statErr != nil {
			return nil, statErr
		}
		if sourceInfo.Mode().Perm() != projection.mode {
			if apply {
				if err := os.Chmod(projection.source, projection.mode); err != nil {
					return nil, err
				}
			} else {
				findings = append(findings, Finding{Change, "projection source " + filepath.Base(projection.source) + " permissions need refresh"})
			}
		}
		contents, readErr := os.ReadFile(projection.source)
		if readErr != nil {
			return nil, fmt.Errorf("read projection source %s: %w", projection.source, readErr)
		}
		info, statErr := os.Lstat(projection.target)
		current := statErr == nil && info.Mode().IsRegular() && info.Mode().Perm() == projection.mode &&
			fileContentsEqual(projection.target, contents)
		if current {
			continue
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
		if statErr == nil && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("refusing to replace non-regular projection target: %s", projection.target)
		}
		if !apply {
			findings = append(findings, Finding{Change, "file projection " + filepath.Base(projection.target) + " needs refresh"})
			continue
		}
		if err := atomicWrite(projection.target, contents, projection.mode); err != nil {
			return nil, err
		}
		findings = append(findings, Finding{Change, "refreshed file projection " + filepath.Base(projection.target)})
	}
	return findings, nil
}

func loadFileProjections(configurationRoot, mount string) ([]fileProjection, error) {
	path := filepath.Join(configurationRoot, ".userland", "files.csv")
	rows, err := csvfile.Read(path, projectionHeader)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]fileProjection, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		source, sourceErr := confinedPath(configurationRoot, row[0], true)
		target, targetErr := confinedPath(mount, row[1], false)
		modeValue, modeErr := strconv.ParseUint(strings.TrimPrefix(row[2], "0o"), 8, 32)
		if sourceErr != nil || targetErr != nil || modeErr != nil || modeValue == 0 || modeValue > 0o777 {
			return nil, fmt.Errorf("invalid file projection at %s:%d", path, index+2)
		}
		if _, exists := seen[target]; exists {
			return nil, fmt.Errorf("duplicate file projection target at %s:%d", path, index+2)
		}
		seen[target] = struct{}{}
		info, statErr := os.Stat(source)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("projection source is not a regular file: %s", source)
		}
		result = append(result, fileProjection{source: source, target: target, mode: os.FileMode(modeValue)})
	}
	return result, nil
}

func confinedPath(root, relative string, resolve bool) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." ||
		relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("path is not a clean relative path")
	}
	candidate := filepath.Join(root, relative)
	if resolve {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", err
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", err
		}
		if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) {
			return "", errors.New("path escapes its root")
		}
		return resolved, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	ancestor := filepath.Dir(candidate)
	for {
		resolvedAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
		if resolveErr == nil {
			if resolvedAncestor != resolvedRoot && !strings.HasPrefix(resolvedAncestor, resolvedRoot+string(os.PathSeparator)) {
				return "", errors.New("path escapes its root")
			}
			break
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", resolveErr
		}
		ancestor = parent
	}
	return candidate, nil
}

func fileContentsEqual(path string, expected []byte) bool {
	actual, err := os.ReadFile(path)
	return err == nil && bytes.Equal(actual, expected)
}
