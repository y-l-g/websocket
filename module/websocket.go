package websocket

// #include <stdlib.h>
// #include "websocket.h"
import "C"
import (
	"unsafe"

	"encoding/json"

	"github.com/dunglas/frankenphp"
)

func init() {
	frankenphp.RegisterExtension(unsafe.Pointer(&C.websocket_module_entry))
}

//export pogo_websocket_publish
func pogo_websocket_publish(appId *C.zend_string, channel *C.zend_string, event *C.zend_string, data *C.zend_string) C.int {
	goAppID := frankenphp.GoString(unsafe.Pointer(appId))
	hubs := GetHubs(goAppID)
	if len(hubs) == 0 {
		return C.int(PublishHubMissing)
	}

	goChannel := frankenphp.GoString(unsafe.Pointer(channel))
	goEvent := frankenphp.GoString(unsafe.Pointer(event))
	goData := frankenphp.GoString(unsafe.Pointer(data))

	return C.int(publishToActiveHubs(hubs, goChannel, goEvent, goData))
}

//export pogo_websocket_broadcast_multi
func pogo_websocket_broadcast_multi(appId *C.zend_string, channels *C.zend_string, event *C.zend_string, data *C.zend_string) C.int {
	goAppID := frankenphp.GoString(unsafe.Pointer(appId))
	hubs := GetHubs(goAppID)
	if len(hubs) == 0 {
		return C.int(PublishHubMissing)
	}

	goChannelsJSON := frankenphp.GoString(unsafe.Pointer(channels))
	goEvent := frankenphp.GoString(unsafe.Pointer(event))
	goData := frankenphp.GoString(unsafe.Pointer(data))

	var channelList []string
	if err := json.Unmarshal([]byte(goChannelsJSON), &channelList); err != nil {
		return C.int(PublishInvalidChannelsJSON)
	}

	status := PublishOK
	for _, ch := range channelList {
		if result := publishToActiveHubs(hubs, ch, goEvent, goData); result != PublishOK && status == PublishOK {
			status = result
		}
	}

	return C.int(status)
}
