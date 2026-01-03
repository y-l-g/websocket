<?php

// Mocks the Laravel Auth Worker in FrankenPHP Worker Mode

ignore_user_abort(true);

$handler = function () {
    // 1. Parse JSON Body
    $input = file_get_contents('php://input');
    $request = json_decode($input, true) ?? [];

    // 2. Retrieve Channel Name
    $channel = $request['channel_name']
        ?? $_SERVER['HTTP_X_FRANKENPHP_WS_CHANNEL']
        ?? '';

    if (!$channel) {
        http_response_code(400);
        file_put_contents('php://stderr', "Worker Error: No channel. Input len: " . strlen($input) . "\n");
        echo json_encode(['error' => 'No channel provided']);
        return;
    }

    // --- TEST SCENARIO: FORBIDDEN CHANNEL ---
    if ($channel === 'private-forbidden') {
        http_response_code(403);
        echo json_encode(['error' => 'Forbidden']);
        return;
    }

    // 3. Mock Auth Response
    if (str_starts_with($channel, 'presence-')) {
        $userId = 'user_' . ($request['socket_id'] ?? 'unknown');

        echo json_encode([
            'auth' => 'test-app:mock_signature',
            'channel_data' => json_encode([
                'user_id' => $userId,
                'user_info' => ['name' => 'Test User ' . $userId],
            ]),
        ]);
    } else {
        echo json_encode([
            'auth' => 'test-app:mock_signature',
        ]);
    }
};

if (function_exists('frankenphp_handle_request')) {
    $running = true;
    while ($running) {
        $running = frankenphp_handle_request($handler);
        gc_collect_cycles();
    }
} else {
    $handler();
}
