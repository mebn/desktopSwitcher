package desktopswitcher

import (
	"os"
	"path/filepath"
	"strings"
)

func enableAutostart() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	commandLine := quoteWindowsArg(exePath)
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
