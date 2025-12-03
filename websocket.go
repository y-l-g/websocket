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


//export frankenphp_websocket_publish
func frankenphp_websocket_publish(channel *C.zend_string, event *C.zend_string, data *C.zend_string) bool {
	hub := GetGlobalHub()
	if hub == nil {
		return false
	}

	// Memory Safety: Convert C strings to Go strings immediately (Copy)
	goChannel := frankenphp.GoString(unsafe.Pointer(channel))
	goEvent := frankenphp.GoString(unsafe.Pointer(event))
	goData := frankenphp.GoString(unsafe.Pointer(data))

	return hub.Publish(goChannel, goEvent, goData)
}

