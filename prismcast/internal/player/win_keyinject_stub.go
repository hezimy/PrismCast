//go:build !windows

package player

func adjustBrowserVolumeByKeypress(pid uint32, delta int) error {
	return nil
}

func terminateProcessByPID(pid uint32) {}

func captureBrowserProcess(hProcess syscall.Handle) uint32 {
	return 0
}
