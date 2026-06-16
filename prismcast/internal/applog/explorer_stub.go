//go:build !windows

package applog

func openWindowsExplorer(dir string) error {
	return execOpen("xdg-open", dir)
}
