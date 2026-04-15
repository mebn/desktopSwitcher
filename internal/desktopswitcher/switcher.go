package desktopswitcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

type Switcher struct {
	dll                     *syscall.DLL
	procIsWindowOnDesktop   *syscall.Proc
	procMoveWindowToDesktop *syscall.Proc
	procGoToDesktopNumber   *syscall.Proc
}

func newSwitcher() (*Switcher, error) {
	dllPath, err := resolveAccessorDLLPath()
	if err != nil {
		return nil, err
	}

	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", filepath.Base(dllPath), err)
	}

	switcher := &Switcher{dll: dll}
	switcher.procIsWindowOnDesktop, err = dll.FindProc("IsWindowOnDesktopNumber")
	if err != nil {
		switcher.Close()
		return nil, fmt.Errorf("find IsWindowOnDesktopNumber: %w", err)
	}
	switcher.procMoveWindowToDesktop, err = dll.FindProc("MoveWindowToDesktopNumber")
	if err != nil {
		switcher.Close()
		return nil, fmt.Errorf("find MoveWindowToDesktopNumber: %w", err)
	}
	switcher.procGoToDesktopNumber, err = dll.FindProc("GoToDesktopNumber")
	if err != nil {
		switcher.Close()
		return nil, fmt.Errorf("find GoToDesktopNumber: %w", err)
	}

	return switcher, nil
}

func (s *Switcher) Close() {
	if s == nil || s.dll == nil {
		return
	}
	_ = s.dll.Release()
	s.dll = nil
	s.procIsWindowOnDesktop = nil
	s.procMoveWindowToDesktop = nil
	s.procGoToDesktopNumber = nil
}

func (s *Switcher) SwitchToDesktop(target int) error {
	state, err := readDesktopState()
	if err == nil {
		if target < 1 || target > state.Count {
			return fmt.Errorf("desktop %d does not exist; Windows reports %d desktop(s)", target, state.Count)
		}
		if target == state.Current {
			return nil
		}
	}

	s.focusTaskbar()
	if err := s.goToDesktop(target - 1); err != nil {
		return err
	}
	_ = s.focusForemostWindowOnDesktop(target)
	return nil
}

func (s *Switcher) MoveCurrentWindowToDesktopAndSwitch(target int) error {
	activeHwnd := getForegroundWindow()
	if activeHwnd == 0 {
		return errors.New("no foreground window is available")
	}

	state, err := readDesktopState()
	if err == nil {
		if target < 1 || target > state.Count {
			return fmt.Errorf("desktop %d does not exist; Windows reports %d desktop(s)", target, state.Count)
		}
		if target == state.Current {
			return nil
		}
	}

	if err := s.moveWindowToDesktop(activeHwnd, target-1); err != nil {
		return err
	}

	s.focusTaskbar()
	if err := s.goToDesktop(target - 1); err != nil {
		return err
	}

	if !isWindowMinimized(activeHwnd) {
		onDesktop, err := s.isWindowOnDesktop(activeHwnd, target-1)
		if err == nil && onDesktop {
			setForegroundWindow(activeHwnd)
			return nil
		}
	}

	_ = s.focusForemostWindowOnDesktop(target)
	return nil
}

func resolveAccessorDLLPath() (string, error) {
	candidates := []string{}

	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), "VirtualDesktopAccessor.dll"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "VirtualDesktopAccessor.dll"))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("VirtualDesktopAccessor.dll was not found next to the executable or in the current working directory")
}

func (s *Switcher) goToDesktop(targetIndex int) error {
	r1, _, callErr := s.procGoToDesktopNumber.Call(uintptr(targetIndex))
	if int32(r1) == -1 {
		return dllCallError("GoToDesktopNumber", callErr)
	}
	return nil
}

func (s *Switcher) moveWindowToDesktop(hwnd uintptr, targetIndex int) error {
	r1, _, callErr := s.procMoveWindowToDesktop.Call(hwnd, uintptr(targetIndex))
	if int32(r1) == -1 {
		return dllCallError("MoveWindowToDesktopNumber", callErr)
	}
	return nil
}

func (s *Switcher) isWindowOnDesktop(hwnd uintptr, targetIndex int) (bool, error) {
	r1, _, callErr := s.procIsWindowOnDesktop.Call(hwnd, uintptr(targetIndex))
	switch int32(r1) {
	case -1:
		return false, dllCallError("IsWindowOnDesktopNumber", callErr)
	case 0:
		return false, nil
	default:
		return true, nil
	}
}

func (s *Switcher) focusForemostWindowOnDesktop(target int) error {
	windows, err := enumTopLevelWindows()
	if err != nil {
		return err
	}

	targetIndex := target - 1
	for _, hwnd := range windows {
		onDesktop, err := s.isWindowOnDesktop(hwnd, targetIndex)
		if err != nil || !onDesktop || isWindowMinimized(hwnd) {
			continue
		}
		setForegroundWindow(hwnd)
		return nil
	}

	return errors.New("no non-minimized window was found on the target desktop")
}

func (s *Switcher) focusTaskbar() {
	className, err := syscall.UTF16PtrFromString("Shell_TrayWnd")
	if err != nil {
		return
	}

	r1, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
	if r1 != 0 {
		setForegroundWindow(r1)
	}
}

func enumTopLevelWindows() ([]uintptr, error) {
	windows := make([]uintptr, 0, 32)
	cb := syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		if isWindowVisible(hwnd) {
			windows = append(windows, hwnd)
		}
		return 1
	})

	r1, _, err := procEnumWindows.Call(cb, 0)
	if r1 == 0 {
		return nil, lastError(err)
	}

	return windows, nil
}

func getForegroundWindow() uintptr {
	r1, _, _ := procGetForegroundWindow.Call()
	return r1
}

func setForegroundWindow(hwnd uintptr) bool {
	r1, _, _ := procSetForegroundWindow.Call(hwnd)
	return r1 != 0
}

func isWindowMinimized(hwnd uintptr) bool {
	r1, _, _ := procIsIconic.Call(hwnd)
	return r1 != 0
}

func isWindowVisible(hwnd uintptr) bool {
	r1, _, _ := procIsWindowVisible.Call(hwnd)
	return r1 != 0
}

func dllCallError(name string, err error) error {
	if err == nil {
		return fmt.Errorf("%s failed", name)
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return fmt.Errorf("%s failed", name)
	}
	return fmt.Errorf("%s failed: %w", name, err)
}
