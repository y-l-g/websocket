<?php

// Trigger a broadcast from PHP to Go

$appId = $_GET['app_id'] ?? 'test-app';
$channel = $_GET['channel'] ?? 'test-channel';
$event = $_GET['event'] ?? 'test-event';
$data = $_GET['data'] ?? '{"hello":"world"}';

if (function_exists('pogo_websocket_publish')) {
    // Cast data to string as the extension expects strings
    $success = pogo_websocket_publish($appId, $channel, $event, (string) $data);
    echo json_encode(['success' => $success]);
} else {
    http_response_code(500);
    echo json_encode(['error' => 'Extension pogo_websocket_publish not loaded']);
}
