package desktopswitcher

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type DesktopState struct {
	Current int
	Count   int
}

func readDesktopState() (DesktopState, error) {
	const desktopsKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\VirtualDesktops`

	desktopIDs, err := queryBinaryValue(hkeyCurrentUser, desktopsKey, "VirtualDesktopIDs")
	if err != nil {
		return DesktopState{Current: 1, Count: 1}, nil
	}

	idLen := 16
	count := len(desktopIDs) / idLen
	if count == 0 {
		return DesktopState{Current: 1, Count: 1}, nil
	}

	currentID, err := queryBinaryValue(hkeyCurrentUser, desktopsKey, "CurrentVirtualDesktop")
	if err != nil || len(currentID) == 0 {
		sessionID, sidErr := currentSessionID()
		if sidErr == nil {
			sessionKey := fmt.Sprintf(`Software\Microsoft\Windows\CurrentVersion\Explorer\SessionInfo\%d\VirtualDesktops`, sessionID)
			currentID, err = queryBinaryValue(hkeyCurrentUser, sessionKey, "CurrentVirtualDesktop")
		}
	}

	if len(currentID) > 0 {
		idLen = len(currentID)
		if idLen > 0 {
			count = len(desktopIDs) / idLen
		}
	}

	if len(currentID) == 0 {
		if count == 1 {
			return DesktopState{Current: 1, Count: 1}, nil
		}
		return DesktopState{}, errors.New("could not read the current virtual desktop from the registry")
	}

	for i := 0; i+idLen <= len(desktopIDs); i += idLen {
		if bytes.Equal(desktopIDs[i:i+idLen], currentID) {
			return DesktopState{Current: i/idLen + 1, Count: count}, nil
		}
	}

	return DesktopState{}, errors.New("current virtual desktop was not found in the registry desktop list")
}

func queryBinaryValue(root uintptr, subkey, value string) ([]byte, error) {
	subkeyPtr, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return nil, err
	}
	valuePtr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return nil, err
	}

	var key uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		root,
		uintptr(unsafe.Pointer(subkeyPtr)),
		0,
		keyQueryValue,
		uintptr(unsafe.Pointer(&key)),
	)
	if r1 != 0 {
		return nil, syscall.Errno(r1)
	}
	defer procRegCloseKey.Call(key)

	var valueType uint32
	var size uint32
	r1, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valuePtr)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 != 0 {
		return nil, syscall.Errno(r1)
	}
	if valueType != regBinary {
		return nil, fmt.Errorf("%s is registry type %d, expected REG_BINARY", value, valueType)
	}
	if size == 0 {
		return nil, nil
	}

	data := make([]byte, size)
	r1, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valuePtr)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 != 0 {
		return nil, syscall.Errno(r1)
	}

	return data[:size], nil
}

func setRegistryStringValue(root uintptr, subkey, valueName, value string) error {
	subkeyPtr, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return err
	}
	valueNamePtr, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}
	valueUTF16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}

	var key uintptr
	r1, _, _ := procRegCreateKeyExW.Call(
		root,
		uintptr(unsafe.Pointer(subkeyPtr)),
		0,
		0,
		0,
		keySetValue,
		0,
		uintptr(unsafe.Pointer(&key)),
		0,
	)
	if r1 != 0 {
		return syscall.Errno(r1)
	}
	defer procRegCloseKey.Call(key)

	r1, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueNamePtr)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&valueUTF16[0])),
		uintptr(len(valueUTF16)*2),
	)
	if r1 != 0 {
		return syscall.Errno(r1)
	}

	return nil
}

func deleteRegistryValue(root uintptr, subkey, valueName string) error {
	subkeyPtr, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return err
	}
	valueNamePtr, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}

	var key uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		root,
		uintptr(unsafe.Pointer(subkeyPtr)),
		0,
		keySetValue,
		uintptr(unsafe.Pointer(&key)),
	)
	if r1 != 0 {
		if r1 == errorFileNotFound {
			return nil
		}
		return syscall.Errno(r1)
	}
	defer procRegCloseKey.Call(key)

	r1, _, _ = procRegDeleteValueW.Call(
		key,
		uintptr(unsafe.Pointer(valueNamePtr)),
	)
	if r1 != 0 && r1 != errorFileNotFound {
		return syscall.Errno(r1)
	}

	return nil
}

func currentSessionID() (uint32, error) {
	var sessionID uint32
	r1, _, err := procProcessIDToSessionID.Call(
		uintptr(os.Getpid()),
		uintptr(unsafe.Pointer(&sessionID)),
	)
	if r1 == 0 {
		return 0, lastError(err)
	}
	return sessionID, nil
}
