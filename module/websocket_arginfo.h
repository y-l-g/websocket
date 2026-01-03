/* This is a generated file, edit the .stub.php file instead.
 * Stub hash: eedab101df2932ad515a371be18aa84c0c76b0f4 */

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_pogo_websocket_publish, 0, 4, _IS_BOOL, 0)
	ZEND_ARG_TYPE_INFO(0, appId, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, channel, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, event, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, data, IS_STRING, 0)
ZEND_END_ARG_INFO()

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_pogo_websocket_broadcast_multi, 0, 4, _IS_BOOL, 0)
	ZEND_ARG_TYPE_INFO(0, appId, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, channels, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, event, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, data, IS_STRING, 0)
ZEND_END_ARG_INFO()

ZEND_FUNCTION(pogo_websocket_publish);
ZEND_FUNCTION(pogo_websocket_broadcast_multi);

static const zend_function_entry ext_functions[] = {
	ZEND_FE(pogo_websocket_publish, arginfo_pogo_websocket_publish)
	ZEND_FE(pogo_websocket_broadcast_multi, arginfo_pogo_websocket_broadcast_multi)
	ZEND_FE_END
};
