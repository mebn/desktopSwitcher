package desktopswitcher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const configFileName = "config.toml"

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

func writeConfig(out io.Writer, cfg Config) error {
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
