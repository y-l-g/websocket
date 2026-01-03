#include <php.h>
#include <Zend/zend_API.h>
#include <Zend/zend_hash.h>
#include <Zend/zend_types.h>
#include <stddef.h>

#include "websocket.h"
#include "websocket_arginfo.h"
#include "_cgo_export.h"


PHP_MINIT_FUNCTION(websocket) {
    
    return SUCCESS;
}

zend_module_entry websocket_module_entry = {STANDARD_MODULE_HEADER,
                                         "websocket",
                                         ext_functions,             /* Functions */
                                         PHP_MINIT(websocket),  /* MINIT */
                                         NULL,                      /* MSHUTDOWN */
                                         NULL,                      /* RINIT */
                                         NULL,                      /* RSHUTDOWN */
                                         NULL,                      /* MINFO */
                                         "1.0.0",                   /* Version */
                                         STANDARD_MODULE_PROPERTIES};
PHP_FUNCTION(pogo_websocket_publish)
{
    zend_string *appId = NULL;
    zend_string *channel = NULL;
    zend_string *event = NULL;
    zend_string *data = NULL;
    ZEND_PARSE_PARAMETERS_START(4, 4)
        Z_PARAM_STR(appId)
        Z_PARAM_STR(channel)
        Z_PARAM_STR(event)
        Z_PARAM_STR(data)
    ZEND_PARSE_PARAMETERS_END();
    int result = pogo_websocket_publish(appId, channel, event, data);
    RETURN_BOOL(result);
}

PHP_FUNCTION(pogo_websocket_broadcast_multi)
{
    zend_string *appId = NULL;
    zend_string *channels = NULL;
    zend_string *event = NULL;
    zend_string *data = NULL;
    ZEND_PARSE_PARAMETERS_START(4, 4)
        Z_PARAM_STR(appId)
        Z_PARAM_STR(channels)
        Z_PARAM_STR(event)
        Z_PARAM_STR(data)
    ZEND_PARSE_PARAMETERS_END();
    int result = pogo_websocket_broadcast_multi(appId, channels, event, data);
    RETURN_BOOL(result);
}

