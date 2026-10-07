//go:build linux && (amd64 || arm64)

package resourcequota

import (
	"fmt"
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Project IDs and inheritance must be provisioned on a quota-enabled volume by
// the deployer. Runtime never enables filesystem features or tags existing data.
var verifiedProjects sync.Map

type projectAttributes struct {
	Flags, ExtSize, Extents, Project, CowExtSize uint32
	Pad                                          [8]byte
}
type diskQuota struct {
	BlockHard, BlockSoft, Space, InodeHard, InodeSoft, Inodes, BlockTime, InodeTime uint64
	Valid                                                                           uint32
	Pad                                                                             uint32
}
type WorkspaceUsage struct {
	Supported                                 bool
	Reason                                    string
	UsedBytes, CapacityBytes, Files, MaxFiles int64
}

func quotaCall(fd uintptr, command, project uint32, value unsafe.Pointer) error {
	_, _, err := unix.Syscall6(unix.SYS_QUOTACTL_FD, fd, uintptr(command), uintptr(project), uintptr(value), 0, 0)
	runtime.KeepAlive(value)
	if err != 0 {
		return err
	}
	return nil
}
func projectID(file *os.File) (uint32, error) {
	var attr projectAttributes
	_, _, err := unix.Syscall(unix.SYS_IOCTL, file.Fd(), 0x801c581f, uintptr(unsafe.Pointer(&attr)))
	runtime.KeepAlive(file)
	if err != 0 {
		return 0, err
	}
	if attr.Project == 0 || attr.Flags&0x200 == 0 {
		return 0, fmt.Errorf("workspace requires a dedicated inherited project quota")
	}
	return attr.Project, nil
}
func WorkspaceStatus(root string) (WorkspaceUsage, error) {
	file, err := os.Open(root)
	if err != nil {
		return WorkspaceUsage{Reason: "workspace_quota_unavailable"}, err
	}
	defer file.Close()
	id, err := projectID(file)
	if err != nil {
		return WorkspaceUsage{Reason: "workspace_project_not_provisioned"}, err
	}
	var q diskQuota
	if err = quotaCall(file.Fd(), 0x80000702, id, unsafe.Pointer(&q)); err != nil {
		return WorkspaceUsage{Reason: "workspace_quota_unavailable"}, err
	}
	return WorkspaceUsage{Supported: true, UsedBytes: int64(q.Space), CapacityBytes: int64(q.BlockHard) * 1024, Files: int64(q.Inodes), MaxFiles: int64(q.InodeHard)}, nil
}
func ApplyWorkspace(root string, v policy.StorageConstraints) (WorkspaceUsage, error) {
	if err := (&policy.Constraints{Storage: &v}).Validate(); err != nil {
		return WorkspaceUsage{}, err
	}
	file, err := os.Open(root)
	if err != nil {
		return WorkspaceUsage{}, err
	}
	defer file.Close()
	id, err := projectID(file)
	if err != nil {
		return WorkspaceUsage{}, err
	}
	// Reject mixed projects and symlinks at the management boundary. Never change
	// ownership of a linked external inode in order to make accounting succeed.
	info, err := file.Stat()
	if err != nil {
		return WorkspaceUsage{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	key := fmt.Sprintf("%d/%d/%d", stat.Dev, stat.Ino, id)
	if _, ok := verifiedProjects.Load(key); !ok {
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			defer f.Close()
			var attr projectAttributes
			_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), 0x801c581f, uintptr(unsafe.Pointer(&attr)))
			if errno != 0 {
				return errno
			}
			if attr.Project != id {
				return fmt.Errorf("workspace contains an inode outside its quota project")
			}
			return nil
		})
		if err != nil {
			return WorkspaceUsage{}, err
		}
		verifiedProjects.Store(key, true)
	}
	current, err := WorkspaceStatus(root)
	if err != nil {
		return WorkspaceUsage{}, err
	}
	if current.CapacityBytes == v.CapacityBytes/1024*1024 && current.MaxFiles == v.MaxFiles {
		return current, nil
	}
	// Kernel quotas use 1 KiB units. Round down so the configured bound is never exceeded.
	if v.CapacityBytes < 1024 {
		return WorkspaceUsage{}, fmt.Errorf("capacity must be at least 1 KiB")
	}
	q := diskQuota{BlockHard: uint64(v.CapacityBytes / 1024), InodeHard: uint64(v.MaxFiles), Valid: 5}
	if err = quotaCall(file.Fd(), 0x80000802, id, unsafe.Pointer(&q)); err != nil {
		return WorkspaceUsage{}, err
	}
	return WorkspaceStatus(root)
}
