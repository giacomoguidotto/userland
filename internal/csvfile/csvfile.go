// Package csvfile owns validated, atomic CSV table persistence.
package csvfile

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Read returns the data records from a CSV file with the exact declared header.
// An empty file represents an empty table.
func Read(path string, header []string) ([][]string, error) {
	if err := validateHeader(header); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = len(header)
	actual, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !slices.Equal(actual, header) {
		return nil, fmt.Errorf("invalid header in %s: got %q, want %q", path, actual, header)
	}

	var rows [][]string
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		rows = append(rows, row)
	}
}

// Write atomically replaces path with a CSV file containing header and rows.
func Write(path string, header []string, rows [][]string, mode os.FileMode) error {
	if err := validateHeader(header); err != nil {
		return err
	}
	var contents bytes.Buffer
	writer := csv.NewWriter(&contents)
	if err := writer.Write(header); err != nil {
		return err
	}
	for index, row := range rows {
		if len(row) != len(header) {
			return fmt.Errorf("invalid record %d for %s: got %d fields, want %d", index+2, path, len(row), len(header))
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return atomicWrite(path, contents.Bytes(), mode)
}

func validateHeader(header []string) error {
	if len(header) == 0 {
		return errors.New("CSV header cannot be empty")
	}
	seen := make(map[string]struct{}, len(header))
	for _, column := range header {
		if column == "" {
			return errors.New("CSV column name cannot be empty")
		}
		if _, exists := seen[column]; exists {
			return fmt.Errorf("duplicate CSV column %q", column)
		}
		seen[column] = struct{}{}
	}
	return nil
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
