package desktopswitcher

import (
	"errors"
	"fmt"
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

type Switcher struct {
	provider *comObject
	manager  *comObject
	variant  virtualDesktopManagerVariant
}

var (
	clsidImmersiveShell                = guid{0xC2F03A33, 0x21F5, 0x47FA, [8]byte{0xB4, 0xBB, 0x15, 0x63, 0x62, 0xA2, 0xF2, 0x39}}
	clsidVirtualDesktopManagerInternal = guid{0xC5E0CDCA, 0x7B6E, 0x41B2, [8]byte{0x9F, 0xC4, 0xD9, 0x39, 0x75, 0xCC, 0x46, 0x7B}}

	iidIServiceProvider = guid{0x6D5140C1, 0x7436, 0x11CE, [8]byte{0x80, 0x34, 0x00, 0xAA, 0x00, 0x60, 0x09, 0xFA}}
	iidIVirtualDesktop  = guid{0x3F07F4BE, 0xB107, 0x441A, [8]byte{0xAF, 0x0F, 0x39, 0xD8, 0x25, 0x29, 0x07, 0x2C}}

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

func newSwitcher() (*Switcher, error) {
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

	switcher := &Switcher{provider: provider}
	for _, variant := range virtualDesktopManagerVariants {
		manager, err := queryService(provider, &clsidVirtualDesktopManagerInternal, &variant.iid)
		if err != nil {
			continue
		}
		switcher.manager = manager
		switcher.variant = variant

		if count, err := switcher.GetDesktopCount(); err == nil && count > 0 {
			return switcher, nil
		}

		releaseCOMObject(manager)
		switcher.manager = nil
	}

	switcher.Close()
	return nil, errors.New("IVirtualDesktopManagerInternal is unavailable")
}

func (s *Switcher) Close() {
	if s == nil {
		return
	}
	releaseCOMObject(s.manager)
	releaseCOMObject(s.provider)
	s.manager = nil
	s.provider = nil
}

func (s *Switcher) SwitchToDesktop(target int) error {
	if s == nil || s.manager == nil {
		return errors.New("desktop switcher is not initialized")
	}

	count, err := s.GetDesktopCount()
	if err != nil {
		return err
	}

	if target < 1 || target > int(count) {
		return fmt.Errorf("desktop %d does not exist; Windows reports %d desktop(s)", target, count)
	}

	desktops, err := s.getDesktops()
	if err != nil {
		return err
	}
	defer releaseCOMObject(desktops)

	desktop, err := objectArrayGetAt(desktops, uint32(target-1), &iidIVirtualDesktop)
	if err != nil {
		return err
	}
	defer releaseCOMObject(desktop)

	if s.variant.hasMonitorArg {
		err = callHRESULT("IVirtualDesktopManagerInternal.SwitchDesktop",
			comVTable(s.manager)[9],
			uintptr(unsafe.Pointer(s.manager)),
			0,
			uintptr(unsafe.Pointer(desktop)),
		)
	} else {
		err = callHRESULT("IVirtualDesktopManagerInternal.SwitchDesktop",
			comVTable(s.manager)[9],
			uintptr(unsafe.Pointer(s.manager)),
			uintptr(unsafe.Pointer(desktop)),
		)
	}
	if err != nil {
		return err
	}

	return nil
}

func (s *Switcher) GetDesktopCount() (uint32, error) {
	var count uint32
	if s.variant.hasMonitorArg {
		err := callHRESULT("IVirtualDesktopManagerInternal.GetDesktopCount",
			comVTable(s.manager)[3],
			uintptr(unsafe.Pointer(s.manager)),
			0,
			uintptr(unsafe.Pointer(&count)),
		)
		return count, err
	}

	err := callHRESULT("IVirtualDesktopManagerInternal.GetDesktopCount",
		comVTable(s.manager)[3],
		uintptr(unsafe.Pointer(s.manager)),
		uintptr(unsafe.Pointer(&count)),
	)
	return count, err
}

func (s *Switcher) getDesktops() (*comObject, error) {
	var desktops *comObject
	var err error

	if s.variant.hasMonitorArg {
		err = callHRESULT("IVirtualDesktopManagerInternal.GetDesktops",
			comVTable(s.manager)[7],
			uintptr(unsafe.Pointer(s.manager)),
			0,
			uintptr(unsafe.Pointer(&desktops)),
		)
	} else {
		err = callHRESULT("IVirtualDesktopManagerInternal.GetDesktops",
			comVTable(s.manager)[7],
			uintptr(unsafe.Pointer(s.manager)),
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
