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
	openConfig       bool
	enableAutostart  bool
	disableAutostart bool
	help             bool

	set *flag.FlagSet
}

func newFlags() *cliFlags {
	options := &cliFlags{}
	flags := flag.NewFlagSet(appName, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&options.openConfig, "open-config", false, "open the TOML config file in VISUAL, EDITOR, or notepad, then exit")
	flags.BoolVar(&options.enableAutostart, "enable-autostart", false, "enable launch at Windows sign-in, then exit")
	flags.BoolVar(&options.disableAutostart, "disable-autostart", false, "disable launch at Windows sign-in, then exit")
	flags.BoolVar(&options.help, "help", false, "show help and exit")
	flags.Usage = options.usage
	options.set = flags
	return options
}

func (f *cliFlags) parse(args []string) error {
	return f.set.Parse(args)
}

func (f *cliFlags) usage() {
	defaultPath, err := resolveConfigPath()
	if err != nil {
		defaultPath = `%APPDATA%\` + appName + `\` + configFileName
	}

	exeName := filepath.Base(os.Args[0])
	fmt.Fprintf(f.set.Output(), `%s switches Windows virtual desktops with global hotkeys.

Usage:
  %s [flags]

Normal run:
  Starts the hotkey listener. If no config exists, it creates:
    %s

Flags:
  --open-config
      Open the resolved config file in default editor, then exit.
  --enable-autostart
      Enable launch at Windows sign-in, then exit.
  --disable-autostart
      Disable launch at Windows sign-in, then exit.
  --help
      Show this help and exit.
`, appName, exeName, defaultPath)
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
