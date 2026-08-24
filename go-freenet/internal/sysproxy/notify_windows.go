//go:build windows

package sysproxy

// notifyWinInet broadcasts WM_SETTINGCHANGE to all top-level windows so that
// running browsers (Chrome, Edge) and other WinInet consumers pick up the
// new proxy settings without requiring a restart.
//
// This mirrors what the Windows "Internet Options" control panel does when you
// apply proxy changes.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
	procInternetSetOption  = windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")
)

const (
	hwndBroadcast                 = uintptr(0xFFFF)
	wmSettingChange               = uintptr(0x001A)
	smstAbortIfHung               = uintptr(0x0002)
	internetOptionSettingsChanged = uintptr(39)
	internetOptionRefresh         = uintptr(37)
)

func notifyWinInet() {
	// 1. Tell WinInet (the proxy engine) that settings changed.
	procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)

	// 2. Broadcast WM_SETTINGCHANGE so Chromium-based browsers and Explorer
	//    pick up the change without requiring a restart.
	setting, _ := windows.UTF16PtrFromString("Internet Settings")
	procSendMessageTimeout.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(setting)),
		smstAbortIfHung,
		uintptr(2000), // 2 second timeout per window
		0,
	)
}
