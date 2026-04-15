//go:build windows

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	appName        = "DesktopSwitcher"
	configFileName = "config.toml"

	sOK    = 0
	sFalse = 1

	coinitApartmentThreaded = 0x2
	clsctxLocalServer       = 0x4

	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	wmHotkey = 0x0312
	wmQuit   = 0x0012

	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002

	vkBack     = 0x08
	vkTab      = 0x09
	vkReturn   = 0x0D
	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12
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
	vkLWin     = 0x5B
	vkRWin     = 0x5C
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
	ole32    = syscall.NewLazyDLL("ole32.dll")

	procRegisterHotKey      = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey    = user32.NewProc("UnregisterHotKey")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procSendInput           = user32.NewProc("SendInput")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")

	procGetCurrentThreadID   = kernel32.NewProc("GetCurrentThreadId")
	procProcessIDToSessionID = kernel32.NewProc("ProcessIdToSessionId")

	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

type Config struct {
	SwitchDelayMs            int
	FocusTaskbarBeforeSwitch bool
	DirectSwitching          bool
	Hotkeys                  map[string]int
}

type configFile struct {
	SwitchDelayMs            *int
	FocusTaskbarBeforeSwitch *bool
	DirectSwitching          *bool
	Hotkeys                  map[string]int
}

type Hotkey struct {
	Spec      string
	VK        uint32
	Modifiers uint32
	Desktop   int
}

type binding struct {
	ID     int
	Hotkey Hotkey
}

type DesktopState struct {
	Current int
	Count   int
}

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

type keyboardInput struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type input struct {
	Type uint32
	_    uint32
	Ki   keyboardInput
	_    [8]byte
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type virtualDesktopManagerVariant struct {
	iid           guid
	name          string
	hasMonitorArg bool
}

type comObject struct {
	vtbl *[32]uintptr
}

type directDesktopSwitcher struct {
	provider       *comObject
	manager        *comObject
	viewCollection *comObject
	variant        virtualDesktopManagerVariant
}

var (
	clsidImmersiveShell                = guid{0xC2F03A33, 0x21F5, 0x47FA, [8]byte{0xB4, 0xBB, 0x15, 0x63, 0x62, 0xA2, 0xF2, 0x39}}
	clsidVirtualDesktopManagerInternal = guid{0xC5E0CDCA, 0x7B6E, 0x41B2, [8]byte{0x9F, 0xC4, 0xD9, 0x39, 0x75, 0xCC, 0x46, 0x7B}}

	iidIServiceProvider           = guid{0x6D5140C1, 0x7436, 0x11CE, [8]byte{0x80, 0x34, 0x00, 0xAA, 0x00, 0x60, 0x09, 0xFA}}
	iidIVirtualDesktop            = guid{0x3F07F4BE, 0xB107, 0x441A, [8]byte{0xAF, 0x0F, 0x39, 0xD8, 0x25, 0x29, 0x07, 0x2C}}
	iidIApplicationView           = guid{0x372E1D3B, 0x38D3, 0x42E4, [8]byte{0xA1, 0x5B, 0x8A, 0xB2, 0xB1, 0x78, 0xF5, 0x13}}
	iidIApplicationViewCollection = guid{0x1841C6D7, 0x4F9D, 0x42C0, [8]byte{0xAF, 0x41, 0x87, 0x47, 0x53, 0x8F, 0x10, 0xE5}}

	virtualDesktopManagerVariants = []virtualDesktopManagerVariant{
		{
			iid:  guid{0x53F5CA0B, 0x158F, 0x4124, [8]byte{0x90, 0x0C, 0x05, 0x71, 0x58, 0x06, 0x0B, 0x27}},
			name: "Windows 11 24H2+",
		},
		{
			iid:  guid{0xC179334C, 0x4295, 0x40D3, [8]byte{0xBE, 0xA1, 0xC6, 0x54, 0xD9, 0x65, 0x60, 0x5A}},
			name: "Windows 10/11 classic",
		},
		{
			iid:           guid{0xF31574D6, 0xB682, 0x4CDC, [8]byte{0xBD, 0x56, 0x18, 0x27, 0x86, 0x0A, 0xBE, 0xC6}},
			name:          "Windows 10 monitor-aware",
			hasMonitorArg: true,
		},
	}
)

func main() {
	runtime.LockOSThread()

	flag.Usage = printUsage
	configPathFlag := flag.String("config", "", "path to a TOML config file")
	openConfigFlag := flag.Bool("open-config", false, "open the TOML config file in VISUAL, EDITOR, or notepad, then exit")
	enableAutostartFlag := flag.Bool("enable-autostart", false, "enable launch at Windows sign-in, then exit")
	disableAutostartFlag := flag.Bool("disable-autostart", false, "disable launch at Windows sign-in, then exit")
	printDefaultConfig := flag.Bool("print-default-config", false, "print the default TOML config and exit")
	helpFlag := flag.Bool("help", false, "show help and exit")
	if err := validateFlagStyle(os.Args[1:]); err != nil {
		fatalf("%v", err)
	}
	flag.Parse()

	if *helpFlag {
		flag.Usage()
		return
	}

	if *enableAutostartFlag && *disableAutostartFlag {
		fatalf("--enable-autostart and --disable-autostart cannot be used together")
	}

	if *printDefaultConfig {
		if err := writeConfig(os.Stdout, defaultConfig()); err != nil {
			fatalf("print default config: %v", err)
		}
		return
	}

	configPath, err := resolveConfigPath(*configPathFlag)
	if err != nil {
		fatalf("resolve config path: %v", err)
	}

	if *disableAutostartFlag {
		if err := disableAutostart(); err != nil {
			fatalf("disable autostart: %v", err)
		}
		fmt.Printf("Autostart disabled for %s.\n", appName)
		return
	}

	if err := ensureConfigFile(configPath); err != nil {
		fatalf("ensure config file: %v", err)
	}

	if *enableAutostartFlag {
		if err := enableAutostart(configPath); err != nil {
			fatalf("enable autostart: %v", err)
		}
		fmt.Printf("Autostart enabled for %s using %s.\n", appName, configPath)
		return
	}

	if *openConfigFlag {
		if err := openConfigInEditor(configPath); err != nil {
			fatalf("open config: %v", err)
		}
		return
	}

	cfg, cfgSource, err := loadConfig(configPath)
	if err != nil {
		fatalf("load config: %v", err)
	}

	hotkeys, err := compileHotkeys(cfg.Hotkeys)
	if err != nil {
		fatalf("parse hotkeys: %v", err)
	}

	if len(hotkeys) == 0 {
		fatalf("no hotkeys configured")
	}

	threadID := getCurrentThreadID()
	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, os.Interrupt)
	go func() {
		<-stopSignals
		postThreadQuit(threadID)
	}()

	registered, err := registerHotkeys(hotkeys)
	if err != nil {
		unregisterHotkeys(registered)
		fatalf("register hotkeys: %v", err)
	}
	defer unregisterHotkeys(registered)

	comInitialized, err := initializeCOM()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Direct desktop switching unavailable: initialize COM: %v\n", err)
	}
	if comInitialized {
		defer procCoUninitialize.Call()
	}

	var directSwitcher *directDesktopSwitcher
	if cfg.DirectSwitching && comInitialized {
		directSwitcher, err = newDirectDesktopSwitcher()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Direct desktop switching unavailable: %v\n", err)
		} else {
			defer directSwitcher.Close()
		}
	}

	switcher := &Switcher{
		switchDelay:            time.Duration(cfg.SwitchDelayMs) * time.Millisecond,
		focusTaskbarBeforeMove: cfg.FocusTaskbarBeforeSwitch,
		direct:                 directSwitcher,
	}

	fmt.Printf("DesktopSwitcher running with %s.\n", cfgSource)
	if directSwitcher != nil {
		fmt.Printf("Direct desktop switching enabled (%s).\n", directSwitcher.variant.name)
	} else {
		fmt.Println("Direct desktop switching disabled; falling back to Win+Ctrl+Arrow stepping.")
	}
	for _, hk := range hotkeys {
		fmt.Printf("  %s -> desktop %d\n", hk.Spec, hk.Desktop)
	}
	fmt.Println("Press Ctrl+C to quit.")

	if err := messageLoop(registered, switcher); err != nil {
		fatalf("message loop: %v", err)
	}
}

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

func defaultConfig() Config {
	hotkeys := make(map[string]int, 9)
	for i := 1; i <= 9; i++ {
		hotkeys[fmt.Sprintf("alt+%d", i)] = i
	}

	return Config{
		SwitchDelayMs:            75,
		FocusTaskbarBeforeSwitch: false,
		DirectSwitching:          true,
		Hotkeys:                  hotkeys,
	}
}

func loadConfig(path string) (Config, string, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", err
	}

	var raw configFile
	if err := parseConfigTOML(string(data), &raw); err != nil {
		return Config{}, "", err
	}

	if raw.SwitchDelayMs != nil {
		cfg.SwitchDelayMs = *raw.SwitchDelayMs
	}
	if raw.FocusTaskbarBeforeSwitch != nil {
		cfg.FocusTaskbarBeforeSwitch = *raw.FocusTaskbarBeforeSwitch
	}
	if raw.DirectSwitching != nil {
		cfg.DirectSwitching = *raw.DirectSwitching
	}
	if raw.Hotkeys != nil {
		cfg.Hotkeys = raw.Hotkeys
	}

	if cfg.SwitchDelayMs < 0 {
		return Config{}, "", errors.New("switchDelayMs cannot be negative")
	}

	return cfg, path, nil
}

func resolveConfigPath(flagPath string) (string, error) {
	if flagPath != "" {
		return filepath.Abs(flagPath)
	}

	if envPath := os.Getenv("DESKTOP_SWITCHER_CONFIG"); envPath != "" {
		return filepath.Abs(envPath)
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(base, appName, configFileName), nil
}

func ensureConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	return writeConfig(file, defaultConfig())
}

func writeConfig(out *os.File, cfg Config) error {
	if _, err := fmt.Fprintf(out, "switchDelayMs = %d\n", cfg.SwitchDelayMs); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "focusTaskbarBeforeSwitch = %t\n", cfg.FocusTaskbarBeforeSwitch); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "directSwitching = %t\n\n", cfg.DirectSwitching); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "[hotkeys]"); err != nil {
		return err
	}

	specs := make([]string, 0, len(cfg.Hotkeys))
	for spec := range cfg.Hotkeys {
		specs = append(specs, spec)
	}
	sort.Strings(specs)

	for _, spec := range specs {
		if _, err := fmt.Fprintf(out, "%q = %d\n", spec, cfg.Hotkeys[spec]); err != nil {
			return err
		}
	}

	return nil
}

func printUsage() {
	defaultPath, err := resolveConfigPath("")
	if err != nil {
		defaultPath = filepath.Join("%APPDATA%", appName, configFileName)
	}

	fmt.Fprintf(flag.CommandLine.Output(), `%s switches Windows virtual desktops with global hotkeys.

Usage:
  %s [flags]

Normal run:
  Starts the hotkey listener. If no config exists, it creates:
    %s

Flags:
  --config <path>
      Path to a TOML config file.
  --open-config
      Open the resolved config file in VISUAL, EDITOR, or notepad, then exit.
  --enable-autostart
      Enable launch at Windows sign-in, then exit.
  --disable-autostart
      Disable launch at Windows sign-in, then exit.
  --print-default-config
      Print the default TOML config and exit.
  --help
      Show this help and exit.

Examples:
  %s
  %s --config C:\path\to\config.toml
  %s --open-config
  %s --enable-autostart
  %s --disable-autostart

Editor selection:
  --open-config uses VISUAL, then EDITOR, then notepad.

Autostart:
  --enable-autostart writes a HKCU Run entry for the current executable.
  The Run command includes --config with the resolved config path.
`, appName, filepath.Base(os.Args[0]), defaultPath, filepath.Base(os.Args[0]), filepath.Base(os.Args[0]), filepath.Base(os.Args[0]), filepath.Base(os.Args[0]), filepath.Base(os.Args[0]))
}

func validateFlagStyle(args []string) error {
	for _, arg := range args {
		if arg == "" || arg == "--" {
			return nil
		}
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
			continue
		}

		name := strings.TrimPrefix(arg, "-")
		if before, _, ok := strings.Cut(name, "="); ok {
			name = before
		}
		if len([]rune(name)) == 1 {
			return fmt.Errorf("single-letter flags are not supported yet; use --help")
		}
		return fmt.Errorf("long flags must use two dashes: use --%s", name)
	}

	return nil
}

func openConfigInEditor(path string) error {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		editor = "notepad"
	}

	parts, err := splitEditorCommand(editor)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return errors.New("editor command is empty")
	}

	args := append(parts[1:], path)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func splitEditorCommand(command string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inQuote := false

	for _, r := range command {
		if r == '"' {
			inQuote = !inQuote
			continue
		}

		if (r == ' ' || r == '\t') && !inQuote {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if inQuote {
		return nil, errors.New("editor command has an unterminated quote")
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts, nil
}

func enableAutostart(configPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return err
	}

	commandLine := quoteWindowsArg(exePath) + " --config " + quoteWindowsArg(configPath)
	return setRegistryStringValue(
		hkeyCurrentUser,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		appName,
		commandLine,
	)
}

func disableAutostart() error {
	return deleteRegistryValue(
		hkeyCurrentUser,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		appName,
	)
}

func quoteWindowsArg(arg string) string {
	if arg == "" {
		return `""`
	}

	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}

	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes*2+1))
			b.WriteRune(r)
			backslashes = 0
		default:
			if backslashes > 0 {
				b.WriteString(strings.Repeat(`\`, backslashes))
				backslashes = 0
			}
			b.WriteRune(r)
		}
	}
	if backslashes > 0 {
		b.WriteString(strings.Repeat(`\`, backslashes*2))
	}
	b.WriteByte('"')
	return b.String()
}

func parseConfigTOML(data string, cfg *configFile) error {
	cfg.Hotkeys = nil
	section := ""

	for lineNo, rawLine := range strings.Split(data, "\n") {
		line := strings.TrimSpace(stripTOMLComment(rawLine))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if section != "hotkeys" {
				return fmt.Errorf("line %d: unknown section %q", lineNo+1, section)
			}
			if cfg.Hotkeys == nil {
				cfg.Hotkeys = map[string]int{}
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("line %d: expected key = value", lineNo+1)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return fmt.Errorf("line %d: expected key = value", lineNo+1)
		}

		parsedKey, err := parseTOMLKey(key)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNo+1, err)
		}

		if section == "hotkeys" {
			desktop, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("line %d: hotkey target must be an integer", lineNo+1)
			}
			if cfg.Hotkeys == nil {
				cfg.Hotkeys = map[string]int{}
			}
			cfg.Hotkeys[parsedKey] = desktop
			continue
		}

		switch parsedKey {
		case "switchDelayMs":
			delay, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("line %d: switchDelayMs must be an integer", lineNo+1)
			}
			cfg.SwitchDelayMs = &delay
		case "focusTaskbarBeforeSwitch":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("line %d: focusTaskbarBeforeSwitch must be true or false", lineNo+1)
			}
			cfg.FocusTaskbarBeforeSwitch = &enabled
		case "directSwitching":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("line %d: directSwitching must be true or false", lineNo+1)
			}
			cfg.DirectSwitching = &enabled
		default:
			return fmt.Errorf("line %d: unknown config key %q", lineNo+1, parsedKey)
		}
	}

	return nil
}

func stripTOMLComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return line[:i]
		}
	}
	return line
}

func parseTOMLKey(key string) (string, error) {
	if strings.HasPrefix(key, "\"") {
		parsed, err := strconv.Unquote(key)
		if err != nil {
			return "", fmt.Errorf("invalid quoted key %q", key)
		}
		return parsed, nil
	}

	return key, nil
}

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

		if err := switcher.SwitchToDesktop(binding.Hotkey.Desktop, binding.Hotkey); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", binding.Hotkey.Spec, err)
		}
	}
}

func initializeCOM() (bool, error) {
	r1, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	switch int32(r1) {
	case sOK, sFalse:
		return true, nil
	default:
		return false, hresultError("CoInitializeEx", r1)
	}
}

func newDirectDesktopSwitcher() (*directDesktopSwitcher, error) {
	var provider *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidImmersiveShell)),
		0,
		clsctxLocalServer,
		uintptr(unsafe.Pointer(&iidIServiceProvider)),
		uintptr(unsafe.Pointer(&provider)),
	)
	if failedHRESULT(hr) {
		return nil, hresultError("CoCreateInstance(CLSID_ImmersiveShell)", hr)
	}
	if provider == nil {
		return nil, errors.New("CoCreateInstance(CLSID_ImmersiveShell) returned a nil IServiceProvider")
	}

	switcher := &directDesktopSwitcher{provider: provider}
	for _, variant := range virtualDesktopManagerVariants {
		manager, err := queryService(provider, &clsidVirtualDesktopManagerInternal, &variant.iid)
		if err != nil {
			continue
		}
		switcher.manager = manager
		switcher.variant = variant

		if count, err := switcher.GetDesktopCount(); err == nil && count > 0 {
			switcher.viewCollection, _ = queryService(provider, &iidIApplicationViewCollection, &iidIApplicationViewCollection)
			return switcher, nil
		}

		releaseCOMObject(manager)
		switcher.manager = nil
	}

	switcher.Close()
	return nil, errors.New("IVirtualDesktopManagerInternal is unavailable")
}

func (d *directDesktopSwitcher) Close() {
	if d == nil {
		return
	}
	releaseCOMObject(d.manager)
	releaseCOMObject(d.viewCollection)
	releaseCOMObject(d.provider)
	d.manager = nil
	d.viewCollection = nil
	d.provider = nil
}

func (d *directDesktopSwitcher) SwitchToDesktop(target int) error {
	if d == nil || d.manager == nil {
		return errors.New("direct desktop switcher is not initialized")
	}

	count, err := d.GetDesktopCount()
	if err != nil {
		return err
	}

	if target < 1 || target > int(count) {
		return fmt.Errorf("desktop %d does not exist; Windows reports %d desktop(s)", target, count)
	}

	desktops, err := d.getDesktops()
	if err != nil {
		return err
	}
	defer releaseCOMObject(desktops)

	desktop, err := objectArrayGetAt(desktops, uint32(target-1), &iidIVirtualDesktop)
	if err != nil {
		return err
	}
	defer releaseCOMObject(desktop)

	if d.variant.hasMonitorArg {
		err = callHRESULT("IVirtualDesktopManagerInternal.SwitchDesktop",
			comVTable(d.manager)[9],
			uintptr(unsafe.Pointer(d.manager)),
			0,
			uintptr(unsafe.Pointer(desktop)),
		)
	} else {
		err = callHRESULT("IVirtualDesktopManagerInternal.SwitchDesktop",
			comVTable(d.manager)[9],
			uintptr(unsafe.Pointer(d.manager)),
			uintptr(unsafe.Pointer(desktop)),
		)
	}
	if err != nil {
		return err
	}

	if err := d.focusTopVisibleViewOnDesktop(desktop); err != nil {
		fmt.Fprintf(os.Stderr, "desktop switched, but focus restore failed: %v\n", err)
	}

	return nil
}

func (d *directDesktopSwitcher) focusTopVisibleViewOnDesktop(desktop *comObject) error {
	if d.viewCollection == nil {
		return errors.New("IApplicationViewCollection is unavailable")
	}
	if desktop == nil {
		return errors.New("target desktop is nil")
	}

	time.Sleep(25 * time.Millisecond)

	var views *comObject
	if err := callHRESULT("IApplicationViewCollection.GetViewsByZOrder",
		comVTable(d.viewCollection)[4],
		uintptr(unsafe.Pointer(d.viewCollection)),
		uintptr(unsafe.Pointer(&views)),
	); err != nil {
		return err
	}
	if views == nil {
		return errors.New("IApplicationViewCollection.GetViewsByZOrder returned nil")
	}
	defer releaseCOMObject(views)

	count, err := objectArrayGetCount(views)
	if err != nil {
		return err
	}

	for i := uint32(0); i < count; i++ {
		view, err := objectArrayGetAt(views, i, &iidIApplicationView)
		if err != nil {
			continue
		}

		visible, err := desktopIsViewVisible(desktop, view)
		if err != nil {
			releaseCOMObject(view)
			continue
		}
		if !visible {
			releaseCOMObject(view)
			continue
		}

		err = activateApplicationView(view)
		releaseCOMObject(view)
		return err
	}

	return errors.New("no visible application view found on the target desktop")
}

func desktopIsViewVisible(desktop, view *comObject) (bool, error) {
	var visible uint32
	err := callHRESULT("IVirtualDesktop.IsViewVisible",
		comVTable(desktop)[3],
		uintptr(unsafe.Pointer(desktop)),
		uintptr(unsafe.Pointer(view)),
		uintptr(unsafe.Pointer(&visible)),
	)
	return visible != 0, err
}

func activateApplicationView(view *comObject) error {
	switchErr := callHRESULT("IApplicationView.SwitchTo",
		comVTable(view)[7],
		uintptr(unsafe.Pointer(view)),
	)
	focusErr := callHRESULT("IApplicationView.SetFocus",
		comVTable(view)[6],
		uintptr(unsafe.Pointer(view)),
	)

	if switchErr == nil || focusErr == nil {
		return nil
	}

	return fmt.Errorf("%v; %v", switchErr, focusErr)
}

func (d *directDesktopSwitcher) GetDesktopCount() (uint32, error) {
	var count uint32
	if d.variant.hasMonitorArg {
		err := callHRESULT("IVirtualDesktopManagerInternal.GetDesktopCount",
			comVTable(d.manager)[3],
			uintptr(unsafe.Pointer(d.manager)),
			0,
			uintptr(unsafe.Pointer(&count)),
		)
		return count, err
	}

	err := callHRESULT("IVirtualDesktopManagerInternal.GetDesktopCount",
		comVTable(d.manager)[3],
		uintptr(unsafe.Pointer(d.manager)),
		uintptr(unsafe.Pointer(&count)),
	)
	return count, err
}

func (d *directDesktopSwitcher) getDesktops() (*comObject, error) {
	var desktops *comObject
	var err error

	if d.variant.hasMonitorArg {
		err = callHRESULT("IVirtualDesktopManagerInternal.GetDesktops",
			comVTable(d.manager)[7],
			uintptr(unsafe.Pointer(d.manager)),
			0,
			uintptr(unsafe.Pointer(&desktops)),
		)
	} else {
		err = callHRESULT("IVirtualDesktopManagerInternal.GetDesktops",
			comVTable(d.manager)[7],
			uintptr(unsafe.Pointer(d.manager)),
			uintptr(unsafe.Pointer(&desktops)),
		)
	}
	if err != nil {
		return nil, err
	}
	if desktops == nil {
		return nil, errors.New("IVirtualDesktopManagerInternal.GetDesktops returned nil")
	}

	return desktops, nil
}

func queryService(provider *comObject, serviceID, interfaceID *guid) (*comObject, error) {
	var obj *comObject
	err := callHRESULT("IServiceProvider.QueryService",
		comVTable(provider)[3],
		uintptr(unsafe.Pointer(provider)),
		uintptr(unsafe.Pointer(serviceID)),
		uintptr(unsafe.Pointer(interfaceID)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("IServiceProvider.QueryService returned nil")
	}
	return obj, nil
}

func objectArrayGetAt(array *comObject, index uint32, interfaceID *guid) (*comObject, error) {
	var obj *comObject
	err := callHRESULT("IObjectArray.GetAt",
		comVTable(array)[4],
		uintptr(unsafe.Pointer(array)),
		uintptr(index),
		uintptr(unsafe.Pointer(interfaceID)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("IObjectArray.GetAt(%d) returned nil", index)
	}
	return obj, nil
}

func objectArrayGetCount(array *comObject) (uint32, error) {
	var count uint32
	err := callHRESULT("IObjectArray.GetCount",
		comVTable(array)[3],
		uintptr(unsafe.Pointer(array)),
		uintptr(unsafe.Pointer(&count)),
	)
	return count, err
}

func comVTable(obj *comObject) *[32]uintptr {
	return obj.vtbl
}

func releaseCOMObject(obj *comObject) {
	if obj == nil {
		return
	}
	syscall.SyscallN(comVTable(obj)[2], uintptr(unsafe.Pointer(obj)))
}

func callHRESULT(name string, proc uintptr, args ...uintptr) error {
	hr, _, _ := syscall.SyscallN(proc, args...)
	if failedHRESULT(hr) {
		return hresultError(name, hr)
	}
	return nil
}

func failedHRESULT(hr uintptr) bool {
	return int32(hr) < 0
}

func hresultError(name string, hr uintptr) error {
	return fmt.Errorf("%s failed with HRESULT 0x%08X", name, uint32(hr))
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

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "desktopswitcher: "+format+"\n", args...)
	os.Exit(1)
}
