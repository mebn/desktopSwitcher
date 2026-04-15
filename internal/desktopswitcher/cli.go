package desktopswitcher

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type cliFlags struct {
	configPath         string
	openConfig         bool
	enableAutostart    bool
	disableAutostart   bool
	printDefaultConfig bool
	help               bool

	set *flag.FlagSet
}

func newFlags() *cliFlags {
	options := &cliFlags{}
	flags := flag.NewFlagSet(appName, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.configPath, "config", "", "path to a TOML config file")
	flags.BoolVar(&options.openConfig, "open-config", false, "open the TOML config file in VISUAL, EDITOR, or notepad, then exit")
	flags.BoolVar(&options.enableAutostart, "enable-autostart", false, "enable launch at Windows sign-in, then exit")
	flags.BoolVar(&options.disableAutostart, "disable-autostart", false, "disable launch at Windows sign-in, then exit")
	flags.BoolVar(&options.printDefaultConfig, "print-default-config", false, "print the default TOML config and exit")
	flags.BoolVar(&options.help, "help", false, "show help and exit")
	flags.Usage = options.usage
	options.set = flags
	return options
}

func (f *cliFlags) parse(args []string) error {
	return f.set.Parse(args)
}

func (f *cliFlags) usage() {
	defaultPath, err := resolveConfigPath("")
	if err != nil {
		defaultPath = filepath.Join("%APPDATA%", appName, configFileName)
	}

	exeName := filepath.Base(os.Args[0])
	fmt.Fprintf(f.set.Output(), `%s switches Windows virtual desktops with global hotkeys.

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
`, appName, exeName, defaultPath, exeName, exeName, exeName, exeName, exeName)
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
