<?php

namespace Pogo\WebSocket;

use Illuminate\Support\Facades\Broadcast;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\ServiceProvider as BaseServiceProvider;
use Pogo\WebSocket\Http\HandshakeController;

class ServiceProvider extends BaseServiceProvider
{
    public function boot()
    {
        // 1. Register the Driver
        Broadcast::extend('frankenphp', function ($app, $config) {
            return new Broadcaster();
        });

        // 2. Register the Route
        Route::middleware('web')
            ->withoutMiddleware([\Illuminate\Foundation\Http\Middleware\VerifyCsrfToken::class])
            ->post('/frankenphp/auth', HandshakeController::class);

        // 3. Register Console Commands
        if ($this->app->runningInConsole()) {
            $this->commands([
                Console\InstallCommand::class,
            ]);
        }
    }
}