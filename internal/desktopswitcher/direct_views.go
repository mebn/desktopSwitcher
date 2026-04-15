package desktopswitcher

import (
	"errors"
	"fmt"
	"time"
	"unsafe"
)

func (d *directDesktopSwitcher) focusTopVisibleViewOnDesktop(desktop *comObject) error {
	if d.viewCollection == nil {
		return errors.New("IApplicationViewCollection is unavailable")
	}
	if desktop == nil {
		return errors.New("target desktop is nil")
	}

	time.Sleep(25 * time.Millisecond)

	var views *comObject
	if err := callHRESULT("IApplicationViewCollection.GetViewsByZOrder",
		comVTable(d.viewCollection)[4],
		uintptr(unsafe.Pointer(d.viewCollection)),
		uintptr(unsafe.Pointer(&views)),
	); err != nil {
		return err
	}
	if views == nil {
		return errors.New("IApplicationViewCollection.GetViewsByZOrder returned nil")
	}
	defer releaseCOMObject(views)

	count, err := objectArrayGetCount(views)
	if err != nil {
		return err
	}

	for i := uint32(0); i < count; i++ {
		view, err := objectArrayGetAt(views, i, &iidIApplicationView)
		if err != nil {
			continue
		}

		visible, err := desktopIsViewVisible(desktop, view)
		if err != nil {
			releaseCOMObject(view)
			continue
		}
		if !visible {
			releaseCOMObject(view)
			continue
		}

		err = activateApplicationView(view)
		releaseCOMObject(view)
		return err
	}

	return errors.New("no visible application view found on the target desktop")
}

func desktopIsViewVisible(desktop, view *comObject) (bool, error) {
	var visible uint32
	err := callHRESULT("IVirtualDesktop.IsViewVisible",
		comVTable(desktop)[3],
		uintptr(unsafe.Pointer(desktop)),
		uintptr(unsafe.Pointer(view)),
		uintptr(unsafe.Pointer(&visible)),
	)
	return visible != 0, err
}

func activateApplicationView(view *comObject) error {
	switchErr := callHRESULT("IApplicationView.SwitchTo",
		comVTable(view)[7],
		uintptr(unsafe.Pointer(view)),
	)
	focusErr := callHRESULT("IApplicationView.SetFocus",
		comVTable(view)[6],
		uintptr(unsafe.Pointer(view)),
	)

	if switchErr == nil || focusErr == nil {
		return nil
	}

	return fmt.Errorf("%v; %v", switchErr, focusErr)
}
