/* This is a generated file, edit the .stub.php file instead.
 * Stub hash: f8c1261e3606ec7a53b8e1f4bcb8123e11db453c */

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_frankenphp_websocket_publish, 0, 3, _IS_BOOL, 0)
	ZEND_ARG_TYPE_INFO(0, channel, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, event, IS_STRING, 0)
	ZEND_ARG_TYPE_INFO(0, data, IS_STRING, 0)
ZEND_END_ARG_INFO()

ZEND_FUNCTION(frankenphp_websocket_publish);

static const zend_function_entry ext_functions[] = {
	ZEND_FE(frankenphp_websocket_publish, arginfo_frankenphp_websocket_publish)
	ZEND_FE_END
};
