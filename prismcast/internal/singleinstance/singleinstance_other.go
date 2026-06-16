//go:build !windows

package singleinstance

func TryAcquire() (alreadyRunning bool, err error) {
	return false, nil
}

func Release() {}

func SetActivateHandler(fn func()) {}

func StartActivationListener() {}

func RequestActivate() (activated bool, err error) {
	return false, nil
}
