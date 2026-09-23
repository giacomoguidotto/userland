package nuke

import "golang.org/x/sys/unix"

func nukeMounts() ([]string, error) {
	count, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	mounts := make([]unix.Statfs_t, count+16)
	count, err = unix.Getfsstat(mounts, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, count)
	for _, mount := range mounts[:count] {
		result = append(result, unix.ByteSliceToString(mount.Mntonname[:]))
	}
	return result, nil
}
