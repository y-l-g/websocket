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
        Broadcast::extend('pogo', function ($app, $config) {
            return new Broadcaster($config);
        });

        Route::middleware('web')
            ->withoutMiddleware([\Illuminate\Foundation\Http\Middleware\VerifyCsrfToken::class])
            ->post('/pogo/auth', HandshakeController::class);

        if ($this->app->runningInConsole()) {
            $this->commands([
                Console\InstallCommand::class,
            ]);
        }
    }
}