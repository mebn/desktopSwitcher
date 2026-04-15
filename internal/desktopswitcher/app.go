package desktopswitcher

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"
)

const appName = "DesktopSwitcher"

func Main(args []string) int {
	if err := Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "desktopswitcher: %v\n", err)
		return 1
	}
	return 0
}

func Run(args []string) error {
	runtime.LockOSThread()

	flags := newFlags()
	if err := validateFlagStyle(args); err != nil {
		return err
	}
	if err := flags.parse(args); err != nil {
		return err
	}

	if flags.help {
		flags.usage()
		return nil
	}

	if flags.enableAutostart && flags.disableAutostart {
		return fmt.Errorf("--enable-autostart and --disable-autostart cannot be used together")
	}

	if flags.printDefaultConfig {
		return writeConfig(os.Stdout, defaultConfig())
	}

	configPath, err := resolveConfigPath(flags.configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	if flags.disableAutostart {
		if err := disableAutostart(); err != nil {
			return fmt.Errorf("disable autostart: %w", err)
		}
		fmt.Printf("Autostart disabled for %s.\n", appName)
		return nil
	}

	if err := ensureConfigFile(configPath); err != nil {
		return fmt.Errorf("ensure config file: %w", err)
	}

	if flags.enableAutostart {
		if err := enableAutostart(configPath); err != nil {
			return fmt.Errorf("enable autostart: %w", err)
		}
		fmt.Printf("Autostart enabled for %s using %s.\n", appName, configPath)
		return nil
	}

	if flags.openConfig {
		if err := openConfigInEditor(configPath); err != nil {
			return fmt.Errorf("open config: %w", err)
		}
		return nil
	}

	cfg, cfgSource, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	hotkeys, err := compileHotkeys(cfg.Hotkeys)
	if err != nil {
		return fmt.Errorf("parse hotkeys: %w", err)
	}
	if len(hotkeys) == 0 {
		return fmt.Errorf("no hotkeys configured")
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
		return fmt.Errorf("register hotkeys: %w", err)
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
		return fmt.Errorf("message loop: %w", err)
	}

	return nil
}
