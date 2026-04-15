package desktopswitcher

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

func queryService(provider *comObject, serviceID, interfaceID *guid) (*comObject, error) {
	var obj *comObject
	err := callHRESULT("IServiceProvider.QueryService",
		comVTable(provider)[3],
		uintptr(unsafe.Pointer(provider)),
		uintptr(unsafe.Pointer(serviceID)),
		uintptr(unsafe.Pointer(interfaceID)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("IServiceProvider.QueryService returned nil")
	}
	return obj, nil
}

func objectArrayGetAt(array *comObject, index uint32, interfaceID *guid) (*comObject, error) {
	var obj *comObject
	err := callHRESULT("IObjectArray.GetAt",
		comVTable(array)[4],
		uintptr(unsafe.Pointer(array)),
		uintptr(index),
		uintptr(unsafe.Pointer(interfaceID)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("IObjectArray.GetAt(%d) returned nil", index)
	}
	return obj, nil
}

func objectArrayGetCount(array *comObject) (uint32, error) {
	var count uint32
	err := callHRESULT("IObjectArray.GetCount",
		comVTable(array)[3],
		uintptr(unsafe.Pointer(array)),
		uintptr(unsafe.Pointer(&count)),
	)
	return count, err
}

func comVTable(obj *comObject) *[32]uintptr {
	return obj.vtbl
}

func releaseCOMObject(obj *comObject) {
	if obj == nil {
		return
	}
	syscall.SyscallN(comVTable(obj)[2], uintptr(unsafe.Pointer(obj)))
}

func callHRESULT(name string, proc uintptr, args ...uintptr) error {
	hr, _, _ := syscall.SyscallN(proc, args...)
	if failedHRESULT(hr) {
		return hresultError(name, hr)
	}
	return nil
}

func failedHRESULT(hr uintptr) bool {
	return int32(hr) < 0
}

func hresultError(name string, hr uintptr) error {
	return fmt.Errorf("%s failed with HRESULT 0x%08X", name, uint32(hr))
}
