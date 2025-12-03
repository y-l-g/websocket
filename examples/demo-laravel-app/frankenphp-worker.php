<?php

define('LARAVEL_START', microtime(true));

require __DIR__ . '/vendor/autoload.php';

$app = require __DIR__ . '/bootstrap/app.php';

// Bootstrap the kernel once
$kernel = $app->make(Illuminate\Contracts\Http\Kernel::class);

// The Worker Handler
$handler = function () use ($kernel, $app) {
    // 1. Capture the synthetic request from Go
    $request = Illuminate\Http\Request::capture();

    $channel = $request->header('X-FrankenPHP-WS-Channel');
    if ($channel) {
        error_log("🔥 PHP WORKER HIT: Checking auth for $channel");
    }
    $response = $kernel->handle($request);

    // 3. Send Output
    foreach ($response->headers->all() as $name => $values) {
        foreach ($values as $value) {
            header("$name: $value", false);
        }
    }
    http_response_code($response->getStatusCode());
    echo $response->getContent();

    // 4. Terminate & Cleanup
    $kernel->terminate($request, $response);

    // // Clear resolved instances to ensure fresh state for next request
    // Illuminate\Support\Facades\Facade::clearResolvedInstances();
};

// Start the Loop
while (frankenphp_handle_request($handler)) {
    gc_collect_cycles();
}