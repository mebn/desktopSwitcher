package desktopswitcher

import "syscall"

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	wmHotkey = 0x0312
	wmQuit   = 0x0012

	vkBack     = 0x08
	vkTab      = 0x09
	vkReturn   = 0x0D
	vkCapsLock = 0x14
	vkEscape   = 0x1B
	vkSpace    = 0x20
	vkPrior    = 0x21
	vkNext     = 0x22
	vkEnd      = 0x23
	vkHome     = 0x24
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkInsert   = 0x2D
	vkDelete   = 0x2E
	vkNumpad0  = 0x60
	vkF1       = 0x70

	keyQueryValue = 0x0001
	keySetValue   = 0x0002
	regBinary     = 3
	regSZ         = 1

	errorFileNotFound = 2

	hkeyCurrentUser = 0x80000001
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegisterHotKey      = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey    = user32.NewProc("UnregisterHotKey")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")

	procGetCurrentThreadID   = kernel32.NewProc("GetCurrentThreadId")
	procProcessIDToSessionID = kernel32.NewProc("ProcessIdToSessionId")

	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

func getCurrentThreadID() uint32 {
	r1, _, _ := procGetCurrentThreadID.Call()
	return uint32(r1)
}

func postThreadQuit(threadID uint32) {
	procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
}

func lastError(err error) error {
	if err == nil {
		return syscall.EINVAL
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return syscall.EINVAL
	}
	return err
}
