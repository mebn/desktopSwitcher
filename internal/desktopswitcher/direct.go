package desktopswitcher

import (
	"errors"
	"fmt"
	"os"
	"unsafe"
)

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
