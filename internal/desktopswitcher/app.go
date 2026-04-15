package desktopswitcher

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
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

	configPath, err := resolveConfigPath()
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
		if err := enableAutostart(); err != nil {
			return fmt.Errorf("enable autostart: %w", err)
		}
		fmt.Printf("Autostart enabled for %s.\n", appName)
		return nil
	}

	if flags.openConfig {
		if err := openConfigInEditor(configPath); err != nil {
			return fmt.Errorf("open config: %w", err)
		}
		return nil
	}

	cfg, _, err := loadConfig(configPath)
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
		return fmt.Errorf("direct desktop switching unavailable: initialize COM: %w", err)
	}
	if comInitialized {
		defer procCoUninitialize.Call()
	}

	switcher, err := newSwitcher()
	if err != nil {
		return fmt.Errorf("direct desktop switching unavailable: %w", err)
	}
	defer switcher.Close()

	if err := messageLoop(registered, switcher); err != nil {
		return fmt.Errorf("message loop: %w", err)
	}

	return nil
}
