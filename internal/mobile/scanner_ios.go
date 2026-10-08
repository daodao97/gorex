//go:build ios && cgo

package mobile

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework AVFoundation
#include <stdlib.h>
void gorex_scan(long long token);
void gorex_hide_keyboard(void);
char *gorex_device_name(void);
char *gorex_device_os(void);
*/
import "C"

import (
	"errors"
	"sync"
	"unsafe"

	"gorex/internal/rex"
)

var scans struct {
	sync.Mutex
	next      int64
	callbacks map[int64]func(string, error)
}

func Scan(done func(string, error)) {
	scans.Lock()
	scans.next++
	id := scans.next
	if scans.callbacks == nil {
		scans.callbacks = make(map[int64]func(string, error))
	}
	scans.callbacks[id] = done
	scans.Unlock()
	C.gorex_scan(C.longlong(id))
}

//export gorexScanResult
func gorexScanResult(token C.longlong, value, message *C.char) {
	scans.Lock()
	done := scans.callbacks[int64(token)]
	delete(scans.callbacks, int64(token))
	scans.Unlock()
	if done == nil {
		return
	}
	var err error
	if message != nil {
		err = errors.New(C.GoString(message))
	}
	done(C.GoString(value), err)
}

func HideKeyboard() { C.gorex_hide_keyboard() }

func DeviceInfo() rex.DeviceInfo {
	name, os := C.gorex_device_name(), C.gorex_device_os()
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(os))
	return rex.DeviceInfo{Name: C.GoString(name), OS: C.GoString(os)}
}
