<?php

namespace Pogo\WebSocket;

use Illuminate\Support\Facades\Broadcast;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\ServiceProvider as BaseServiceProvider;
use Pogo\WebSocket\Http\HandshakeController;
use Pogo\WebSocket\Http\UserAuthController;

class ServiceProvider extends BaseServiceProvider
{
    public function boot(): void
    {
        Broadcast::extend('pogo', function ($app, $config) {
            /** @var array<string, mixed> $configArray */
            $configArray = is_array($config) ? $config : [];
            return new Broadcaster($configArray);
        });

        Route::middleware('web')
            ->withoutMiddleware([\Illuminate\Foundation\Http\Middleware\VerifyCsrfToken::class])
            ->group(function () {
                Route::post('/pogo/auth', HandshakeController::class);
                Route::post('/pogo/user-auth', UserAuthController::class);
            });

        if ($this->app->runningInConsole()) {
            $this->commands([
                Console\InstallCommand::class,
            ]);
        }
    }
}
