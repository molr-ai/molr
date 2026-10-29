package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// publishFile commits a completed transfer only while the confirmed destination
// is unchanged. The caller owns confirmation and transfer progress.
func publishFile(ctx context.Context, target string, expected os.FileInfo, write func(io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".molr-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = write(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := os.Lstat(target)
	if expected == nil {
		if err == nil {
			return fmt.Errorf("目标文件刚刚被创建，请重新确认")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = os.Link(f.Name(), target); err != nil {
			return err
		}
		return nil
	}
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(expected, current) || current.Size() != expected.Size() || !current.ModTime().Equal(expected.ModTime()) {
		return fmt.Errorf("目标文件已变化，请重新确认替换")
	}
	return os.Rename(f.Name(), target)
}
