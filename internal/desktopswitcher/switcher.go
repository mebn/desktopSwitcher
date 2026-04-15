package desktopswitcher

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

type Switcher struct {
	switchDelay            time.Duration
	focusTaskbarBeforeMove bool
	direct                 *directDesktopSwitcher
}

func (s *Switcher) SwitchToDesktop(target int, trigger Hotkey) error {
	if s.direct != nil {
		if err := s.direct.SwitchToDesktop(target); err == nil {
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "direct switch failed; falling back to Win+Ctrl+Arrow: %v\n", err)
		}
	}

	state, err := readDesktopState()
	if err != nil {
		return err
	}

	if target < 1 || target > state.Count {
		return fmt.Errorf("desktop %d does not exist; Windows reports %d desktop(s)", target, state.Count)
	}

	if target == state.Current {
		return nil
	}

	if s.focusTaskbarBeforeMove {
		focusTaskbar()
	}

	// If the hotkey modifier is still logically down, it can become an extra
	// modifier on Win+Ctrl+Left/Right. Release it before sending navigation.
	_ = releaseModifiers(trigger.Modifiers)
	time.Sleep(15 * time.Millisecond)

	direction := uint16(vkRight)
	steps := target - state.Current
	if steps < 0 {
		direction = uint16(vkLeft)
		steps = -steps
	}

	for i := 0; i < steps; i++ {
		if err := sendDesktopStep(direction); err != nil {
			return err
		}
		if i < steps-1 {
			time.Sleep(s.switchDelay)
		}
	}

	return nil
}

func focusTaskbar() {
	className, err := syscall.UTF16PtrFromString("Shell_TrayWnd")
	if err != nil {
		return
	}

	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
	if hwnd != 0 {
		procSetForegroundWindow.Call(hwnd)
	}
}

func releaseModifiers(modifiers uint32) error {
	var inputs []input
	if modifiers&modAlt != 0 {
		inputs = append(inputs, keyUp(vkMenu))
	}
	if modifiers&modControl != 0 {
		inputs = append(inputs, keyUp(vkControl))
	}
	if modifiers&modShift != 0 {
		inputs = append(inputs, keyUp(vkShift))
	}
	if modifiers&modWin != 0 {
		inputs = append(inputs, keyUp(vkLWin), keyUp(vkRWin))
	}
	if len(inputs) == 0 {
		return nil
	}
	return sendInputs(inputs)
}

func sendDesktopStep(direction uint16) error {
	inputs := []input{
		keyDown(vkLWin),
		keyDown(vkControl),
		keyDown(direction),
		keyUp(direction),
		keyUp(vkControl),
		keyUp(vkLWin),
	}
	return sendInputs(inputs)
}

func keyDown(vk uint16) input {
	return input{
		Type: inputKeyboard,
		Ki: keyboardInput{
			WVk: vk,
		},
	}
}

func keyUp(vk uint16) input {
	return input{
		Type: inputKeyboard,
		Ki: keyboardInput{
			WVk:     vk,
			DwFlags: keyeventfKeyUp,
		},
	}
}

func sendInputs(inputs []input) error {
	if len(inputs) == 0 {
		return nil
	}

	r1, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if int(r1) != len(inputs) {
		return fmt.Errorf("SendInput sent %d of %d inputs: %w", r1, len(inputs), lastError(err))
	}
	return nil
}
