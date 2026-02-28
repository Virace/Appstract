//go:build windows

package winui

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	mbOKCancel   = 0x00000001
	mbIconInfo   = 0x00000040
	idOK         = 1
	messageTitle = "Appstract"
)

func ConfirmUpdateReady(appName, version string) (bool, error) {
	text := fmt.Sprintf("[%s] 新版本 v%s 已准备就绪，是否立即重启更新？", appName, version)
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	ret, _, callErr := proc.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(messageTitle))),
		uintptr(mbOKCancel|mbIconInfo),
	)
	if ret == 0 {
		return false, callErr
	}
	return ret == idOK, nil
}
