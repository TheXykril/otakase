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
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
	"unsafe"

	"github.com/thexykril/otakase/mobilecore"
	"github.com/thexykril/otakase/mobilecore/tvupdate"
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

// otakase_tv_update checks for and installs a signed app update (see
// package tvupdate). It blocks; the caller frees the JSON result
// ({"data": ...} or {"error": "..."}) with otakase_core_free.
//
//export otakase_tv_update
func otakase_tv_update(input *C.char) *C.char {
	return C.CString(tvUpdate(C.GoString(input)))
}

func tvUpdate(input string) string {
	var req tvupdate.Request
	out := map[string]any{}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		out["error"] = "invalid request"
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		client := &http.Client{Timeout: 15 * time.Minute}
		if res, err := tvupdate.Run(ctx, client, req, tvupdate.PublicKey); err != nil {
			out["error"] = err.Error()
		} else {
			out["data"] = res
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func main() {}
