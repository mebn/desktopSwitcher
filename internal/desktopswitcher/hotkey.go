package desktopswitcher

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

type Hotkey struct {
	Spec      string
	VK        uint32
	Modifiers uint32
	Desktop   int
	Action    HotkeyAction
}

type binding struct {
	ID     int
	Hotkey Hotkey
}

type HotkeyAction int

const (
	hotkeyActionSwitch HotkeyAction = iota
	hotkeyActionMoveAndFollow
)

func compileHotkeys(configured map[string]int) ([]Hotkey, error) {
	specs := make([]string, 0, len(configured))
	for spec := range configured {
		specs = append(specs, spec)
	}
	sort.Strings(specs)

	seen := map[string]bool{}
	hotkeys := make([]Hotkey, 0, len(specs))
	for _, spec := range specs {
		desktop := configured[spec]
		if desktop < 1 || desktop > 9 {
			return nil, fmt.Errorf("%q targets desktop %d; supported desktop numbers are 1-9", spec, desktop)
		}

		hk, err := parseHotkey(spec)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", spec, err)
		}
		hk.Spec = normalizeHotkeySpec(spec)
		hk.Desktop = desktop
		hk.Action = hotkeyActionForModifiers(hk.Modifiers)

		key := fmt.Sprintf("%d:%d", hk.Modifiers, hk.VK)
		if seen[key] {
			return nil, fmt.Errorf("duplicate hotkey %q", spec)
		}
		seen[key] = true
		hotkeys = append(hotkeys, hk)
	}

	return hotkeys, nil
}

func parseHotkey(spec string) (Hotkey, error) {
	parts := strings.Split(spec, "+")
	if len(parts) == 0 {
		return Hotkey{}, errors.New("empty hotkey")
	}

	var hk Hotkey
	var keySet bool
	for _, part := range parts {
		token := strings.TrimSpace(strings.ToLower(part))
		if token == "" {
			return Hotkey{}, errors.New("empty token")
		}

		if mod, ok := modifierByName(token); ok {
			hk.Modifiers |= mod
			continue
		}

		if keySet {
			return Hotkey{}, errors.New("hotkeys must contain one non-modifier key")
		}

		vk, err := virtualKeyByName(token)
		if err != nil {
			return Hotkey{}, err
		}
		hk.VK = vk
		keySet = true
	}

	if !keySet {
		return Hotkey{}, errors.New("missing non-modifier key")
	}

	return hk, nil
}

func normalizeHotkeySpec(spec string) string {
	parts := strings.Split(spec, "+")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(strings.ToLower(part))
		if token != "" {
			out = append(out, token)
		}
	}
	return strings.Join(out, "+")
}

func modifierByName(name string) (uint32, bool) {
	switch name {
	case "alt", "menu":
		return modAlt, true
	case "ctrl", "control":
		return modControl, true
	case "shift":
		return modShift, true
	case "win", "windows", "super", "cmd":
		return modWin, true
	default:
		return 0, false
	}
}

func hotkeyActionForModifiers(modifiers uint32) HotkeyAction {
	if modifiers&modAlt != 0 && modifiers&modShift != 0 {
		return hotkeyActionMoveAndFollow
	}
	return hotkeyActionSwitch
}

func virtualKeyByName(name string) (uint32, error) {
	if len(name) == 1 {
		ch := name[0]
		switch {
		case ch >= 'a' && ch <= 'z':
			return uint32(ch - 'a' + 'A'), nil
		case ch >= '0' && ch <= '9':
			return uint32(ch), nil
		}
	}

	if strings.HasPrefix(name, "f") {
		n, err := strconv.Atoi(strings.TrimPrefix(name, "f"))
		if err == nil && n >= 1 && n <= 24 {
			return vkF1 + uint32(n-1), nil
		}
	}

	if strings.HasPrefix(name, "num") {
		n, err := strconv.Atoi(strings.TrimPrefix(name, "num"))
		if err == nil && n >= 0 && n <= 9 {
			return vkNumpad0 + uint32(n), nil
		}
	}

	names := map[string]uint32{
		"backspace": vkBack,
		"back":      vkBack,
		"tab":       vkTab,
		"enter":     vkReturn,
		"return":    vkReturn,
		"esc":       vkEscape,
		"escape":    vkEscape,
		"space":     vkSpace,
		"pageup":    vkPrior,
		"pgup":      vkPrior,
		"pagedown":  vkNext,
		"pgdn":      vkNext,
		"end":       vkEnd,
		"home":      vkHome,
		"left":      vkLeft,
		"up":        vkUp,
		"right":     vkRight,
		"down":      vkDown,
		"insert":    vkInsert,
		"ins":       vkInsert,
		"delete":    vkDelete,
		"del":       vkDelete,
		"capslock":  vkCapsLock,
		"multiply":  0x6A,
		"add":       0x6B,
		"plus":      0xBB,
		"subtract":  0x6D,
		"decimal":   0x6E,
		"divide":    0x6F,
		"semicolon": 0xBA,
		"equals":    0xBB,
		"comma":     0xBC,
		"minus":     0xBD,
		"period":    0xBE,
		"slash":     0xBF,
		"backtick":  0xC0,
		"lbracket":  0xDB,
		"backslash": 0xDC,
		"rbracket":  0xDD,
		"quote":     0xDE,
	}

	if vk, ok := names[name]; ok {
		return vk, nil
	}

	return 0, fmt.Errorf("unknown key %q", name)
}

func registerHotkeys(hotkeys []Hotkey) (map[uintptr]binding, error) {
	registered := make(map[uintptr]binding, len(hotkeys))
	for i, hk := range hotkeys {
		id := i + 1
		r1, _, err := procRegisterHotKey.Call(
			0,
			uintptr(id),
			uintptr(hk.Modifiers|modNoRepeat),
			uintptr(hk.VK),
		)
		if r1 == 0 {
			return registered, fmt.Errorf("%s -> desktop %d: %w", hk.Spec, hk.Desktop, lastError(err))
		}

		registered[uintptr(id)] = binding{ID: id, Hotkey: hk}
	}

	return registered, nil
}

func unregisterHotkeys(bindings map[uintptr]binding) {
	for _, binding := range bindings {
		procUnregisterHotKey.Call(0, uintptr(binding.ID))
	}
}

func messageLoop(bindings map[uintptr]binding, switcher *Switcher) error {
	var m msg
	for {
		r1, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r1) {
		case -1:
			return lastError(err)
		case 0:
			return nil
		}

		if m.Message != wmHotkey {
			continue
		}

		binding, ok := bindings[m.WParam]
		if !ok {
			continue
		}

		var switchErr error
		switch binding.Hotkey.Action {
		case hotkeyActionMoveAndFollow:
			switchErr = switcher.MoveCurrentWindowToDesktopAndSwitch(binding.Hotkey.Desktop)
		default:
			switchErr = switcher.SwitchToDesktop(binding.Hotkey.Desktop)
		}

		if switchErr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", binding.Hotkey.Spec, switchErr)
		}
	}
}
