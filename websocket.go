package websocket

// #include <stdlib.h>
// #include "websocket.h"
import "C"
import (
	"unsafe"

	"github.com/dunglas/frankenphp"
)

func init() {
	frankenphp.RegisterExtension(unsafe.Pointer(&C.websocket_module_entry))
}

//export pogo_websocket_publish
func pogo_websocket_publish(appId *C.zend_string, channel *C.zend_string, event *C.zend_string, data *C.zend_string) bool {
	// Memory Safety: Convert C strings to Go strings immediately (Copy)
	goAppID := frankenphp.GoString(unsafe.Pointer(appId))

	hub := GetHub(goAppID)
	if hub == nil {
		return false
	}

	goChannel := frankenphp.GoString(unsafe.Pointer(channel))
	goEvent := frankenphp.GoString(unsafe.Pointer(event))
	goData := frankenphp.GoString(unsafe.Pointer(data))

	return hub.Publish(goChannel, goEvent, goData)
}
