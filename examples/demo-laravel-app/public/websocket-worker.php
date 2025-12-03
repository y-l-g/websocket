<?php

use Illuminate\Contracts\Http\Kernel;
use Illuminate\Container\Container;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Facade;

if ((!($_SERVER['FRANKENPHP_WORKER'] ?? false)) || !function_exists('frankenphp_handle_request')) {
    exit(1);
}

define('LARAVEL_START', microtime(true));

require __DIR__ . '/../vendor/autoload.php';

$baseApp = require __DIR__ . '/../bootstrap/app.php';

$baseApp->make(Kernel::class)->bootstrap();

$requestCount = 0;
$maxRequests = $_ENV['MAX_REQUESTS'] ?? $_SERVER['MAX_REQUESTS'] ?? 1000;

$handler = function () use ($baseApp) {
    $app = clone $baseApp;

    Container::setInstance($app);
    Facade::setFacadeApplication($app);

    $request = Request::capture();
    $app->instance('request', $request);

    $kernel = $app->make(Kernel::class);

    $response = $kernel->handle($request);

    foreach ($response->headers->all() as $name => $values) {
        foreach ($values as $value) {
            header("$name: $value", false);
        }
    }

    http_response_code($response->getStatusCode());
    echo $response->getContent();

    $kernel->terminate($request, $response);

    unset($app, $kernel, $request, $response);

    Container::setInstance($baseApp);
    Facade::setFacadeApplication($baseApp);
};

while ($requestCount < $maxRequests && frankenphp_handle_request($handler)) {
    $requestCount++;
}

gc_collect_cycles();