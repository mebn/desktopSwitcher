package desktopswitcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const configFileName = "config.toml"

type Config struct {
	Hotkeys map[string]int
}

type configFile struct {
	Hotkeys map[string]int
}

func defaultConfig() string {
	return `[hotkeys]
"alt+1" = 1
"alt+2" = 2
"alt+3" = 3
"alt+4" = 4
"alt+5" = 5
"alt+6" = 6
"alt+7" = 7
"alt+8" = 8
"alt+9" = 9
"alt+shift+1" = 1
"alt+shift+2" = 2
"alt+shift+3" = 3
"alt+shift+4" = 4
"alt+shift+5" = 5
"alt+shift+6" = 6
"alt+shift+7" = 7
"alt+shift+8" = 8
"alt+shift+9" = 9
`
}

func loadConfig(path string) (Config, string, error) {
	cfg := Config{
		Hotkeys: map[string]int{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", err
	}

	var raw configFile
	if err := parseConfigTOML(string(data), &raw); err != nil {
		return Config{}, "", err
	}

	if raw.Hotkeys != nil {
		cfg.Hotkeys = raw.Hotkeys
	}

	return cfg, path, nil
}

func resolveConfigPath() (string, error) {
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

	_, err = file.WriteString(defaultConfig())
	return err
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

		return fmt.Errorf("line %d: unknown config key %q", lineNo+1, parsedKey)
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
