package websocket

// #include <stdlib.h>
// #include "websocket.h"
import "C"
import (
	"unsafe"

	"encoding/json"
	"time"

	"github.com/dunglas/frankenphp"
)

func init() {
	frankenphp.RegisterExtension(unsafe.Pointer(&C.websocket_module_entry))
}

//export pogo_websocket_publish
func pogo_websocket_publish(appId *C.zend_string, channel *C.zend_string, event *C.zend_string, data *C.zend_string) bool {
	entryAt := time.Now()
	goAppID := frankenphp.GoString(unsafe.Pointer(appId))
	hub := GetHub(goAppID)
	if hub == nil {
		return false
	}

	goChannel := frankenphp.GoString(unsafe.Pointer(channel))
	goEvent := frankenphp.GoString(unsafe.Pointer(event))
	goData := frankenphp.GoString(unsafe.Pointer(data))

	return hub.publish(goChannel, goEvent, goData, entryAt)
}

//export pogo_websocket_broadcast_multi
func pogo_websocket_broadcast_multi(appId *C.zend_string, channels *C.zend_string, event *C.zend_string, data *C.zend_string) bool {
	entryAt := time.Now()
	goAppID := frankenphp.GoString(unsafe.Pointer(appId))
	hub := GetHub(goAppID)
	if hub == nil {
		return false
	}

	goChannelsJSON := frankenphp.GoString(unsafe.Pointer(channels))
	goEvent := frankenphp.GoString(unsafe.Pointer(event))
	goData := frankenphp.GoString(unsafe.Pointer(data))

	var channelList []string
	if err := json.Unmarshal([]byte(goChannelsJSON), &channelList); err != nil {
		return false
	}

	success := true
	for _, ch := range channelList {
		if !hub.publish(ch, goEvent, goData, entryAt) {
			success = false
		}
	}

	return success
}
