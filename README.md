# DesktopSwitcher

A small Windows Go program for switching virtual desktops with global hotkeys.

By default it registers:

```text
Alt+1 -> desktop 1
Alt+2 -> desktop 2
...
Alt+9 -> desktop 9
```

It does not use AutoHotkey or `VirtualDesktopAccessor.dll`.

## Build

```powershell
go build -o desktopswitcher.exe .
```

Run it:

```powershell
.\desktopswitcher.exe
```

For a no-console build:

```powershell
go build -ldflags="-H=windowsgui" -o desktopswitcher.exe .
```

Use the console build first while testing, because it prints registration errors if another app already owns a hotkey.

## Configuration

On first run, the program creates the default config here:

```text
C:\Users\<username>\AppData\Roaming\DesktopSwitcher\config.toml
```

To use another config file, pass a path explicitly:

```powershell
.\desktopswitcher.exe --config C:\path\to\config.toml
```

Example matching your current AutoHotkey layout:

```toml
switchDelayMs = 75
focusTaskbarBeforeSwitch = false
directSwitching = true

[hotkeys]
"alt+1" = 1
"alt+2" = 2
"alt+3" = 3
"alt+q" = 4
"alt+w" = 5
"alt+e" = 6
"alt+a" = 7
"alt+s" = 8
"alt+d" = 9
```

Supported modifier names are `alt`, `ctrl`, `shift`, and `win`. Supported key names include letters, digits, `f1` through `f24`, `space`, `tab`, `enter`, `esc`, arrow keys, `home`, `end`, `pageup`, `pagedown`, `insert`, `delete`, `capslock`, and `num0` through `num9`.

You can also print the default config:

```powershell
.\desktopswitcher.exe --print-default-config
```

Open the resolved config file in your editor:

```powershell
.\desktopswitcher.exe --open-config
```

`--open-config` uses `VISUAL`, then `EDITOR`, then `notepad`.

Enable or disable Windows sign-in autostart:

```powershell
.\desktopswitcher.exe --enable-autostart
.\desktopswitcher.exe --disable-autostart
```

Autostart uses the current executable path and includes `--config` with the resolved config path. Use `--config C:\path\to\config.toml --enable-autostart` if you want autostart to use a custom config.

Show help:

```powershell
.\desktopswitcher.exe --help
```

## Notes

Windows does not expose a stable public API for directly jumping to virtual desktop number N. By default this program uses Explorer's internal virtual desktop COM service for direct switching. After switching, it asks Explorer's application view collection to activate the target desktop's last active visible view. If the internal service is unavailable, or if `directSwitching = false`, it falls back to reading Explorer's virtual desktop registry keys and sending `Win+Ctrl+Left` or `Win+Ctrl+Right` the required number of times.

Only existing desktops can be selected. If you press a hotkey for desktop 6 and Windows currently has only 3 desktops, the program leaves you where you are and prints an error in the console build.
