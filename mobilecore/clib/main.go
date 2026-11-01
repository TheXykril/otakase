// Command clib builds the mobile core as a C shared library for platforms
// without gomobile, such as Samsung (Tizen) TVs. The app loads it with
// dart:ffi. The functions mirror mobilecore.Core one to one.
//
//	go build -buildmode=c-shared -o libotakasecore.so ./mobilecore/clib
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/thexykril/otakase/mobilecore"
)

var (
	coreOnce sync.Once
	core     *mobilecore.Core
)

func shared() *mobilecore.Core {
	coreOnce.Do(func() { core = mobilecore.NewCore() })
	return core
}

// otakase_core_start schedules a JSON request and returns its handle, or 0
// when the core is closed or busy.
//
//export otakase_core_start
func otakase_core_start(input *C.char) C.longlong {
	return C.longlong(shared().Start(C.GoString(input)))
}

// otakase_core_result blocks until the request finishes. The caller owns the
// returned string and must release it with otakase_core_free.
//
//export otakase_core_result
func otakase_core_result(id C.longlong) *C.char {
	return C.CString(shared().Result(int64(id)))
}

//export otakase_core_cancel
func otakase_core_cancel(id C.longlong) {
	shared().Cancel(int64(id))
}

//export otakase_core_stop_playback
func otakase_core_stop_playback() {
	shared().StopPlayback()
}

//export otakase_core_free
func otakase_core_free(value *C.char) {
	C.free(unsafe.Pointer(value))
}

func main() {}
