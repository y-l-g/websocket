/* This is a generated file, edit the .stub.php file instead.
 * Stub hash: c1bd924fd283cec8d49a0cfe0fe7b69812eba417 */

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_frankenphp_websocket_publish, 0, 4, _IS_BOOL, 0)
	ZEND_ARG_TYPE_INFO(0, appId, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, channel, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, event, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, data, IS_STRING, 0)
ZEND_END_ARG_INFO()

ZEND_FUNCTION(frankenphp_websocket_publish);

static const zend_function_entry ext_functions[] = {
	ZEND_FE(frankenphp_websocket_publish, arginfo_frankenphp_websocket_publish)
	ZEND_FE_END
};
