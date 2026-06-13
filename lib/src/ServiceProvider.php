<?php

namespace Pogo\WebSocket;

use Illuminate\Support\Facades\Broadcast;
use Illuminate\Support\ServiceProvider as BaseServiceProvider;

class ServiceProvider extends BaseServiceProvider
{
    public function boot(): void
    {
        Broadcast::extend('pogo', function ($app, $config) {
            /** @var array<string, mixed> $configArray */
            $configArray = is_array($config) ? $config : [];

            return new Broadcaster($configArray);
        });

        if ($this->app->runningInConsole()) {
            $this->commands([
                Console\InstallCommand::class,
            ]);
        }
    }
}
