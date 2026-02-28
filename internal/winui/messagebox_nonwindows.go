//go:build !windows

package winui

func ConfirmUpdateReady(appName, version string) (bool, error) {
	return true, nil
}
