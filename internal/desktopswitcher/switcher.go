package desktopswitcher

import (
	"errors"
)

type Switcher struct {
	direct *directDesktopSwitcher
}

func (s *Switcher) SwitchToDesktop(target int) error {
	if s.direct == nil {
		return errors.New("direct desktop switcher is not initialized")
	}

	return s.direct.SwitchToDesktop(target)
}
