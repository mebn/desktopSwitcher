# DesktopSwitcher

A small Windows Go program for switching virtual desktops with global hotkeys.

This executable uses the bundled `VirtualDesktopAccessor.dll` at runtime. Keep the DLL next to the executable.

## Usage

By default, `Alt+<num>` switches to the corresponding desktop, where `<num>` is a number from 1 to 9.

`Alt+Shift+<num>` moves the currently focused window to that desktop and follows it there.

## Configuration

On first run, the program creates the default config here:

```text
C:\Users\<username>\AppData\Roaming\DesktopSwitcher\config.toml
```

Open the config file in your default editor:

```powershell
.\desktopswitcher.exe --open-config
```
