<?php

namespace Pogo\WebSocket\Console;

use Illuminate\Console\Command;
use Illuminate\Support\Facades\File;

class InstallCommand extends Command
{
    protected $signature = 'pogo:ws-install';
    protected $description = 'Install Pogo WebSocket engine';

    public function handle()
    {
        $this->components->info('Installing Pogo WebSocket Engine...');

        if ($this->confirm('Run Laravel broadcasting scaffolding?', true)) {
            $this->call('install:broadcasting', [
                '--pusher' => true,
                '--no-interaction' => true,
            ]);
        }

        $this->updateBroadcastingConfig();
        $this->updateEnvironment();
        $this->updateFrontend();

        $this->newLine();
        $this->components->info('Pogo WebSocket installed successfully. 🐘');
    }

    protected function updateBroadcastingConfig()
    {
        $configPath = config_path('broadcasting.php');
        if (!File::exists($configPath))
            return;

        $content = File::get($configPath);

        if (!str_contains($content, "'frankenphp' => [")) {
            $driverConfig = <<<'CONFIG'
        'frankenphp' => [
            'driver' => 'frankenphp',
            'app_id' => env('WS_APP_ID', 'frankenphp-app'),
        ],

CONFIG;
            $content = preg_replace(
                "/'connections' => \[\n/",
                "'connections' => [\n" . $driverConfig,
                $content
            );
            File::put($configPath, $content);
        }
    }

    protected function updateEnvironment()
    {
        $envPath = base_path('.env');
        if (!File::exists($envPath))
            return;

        $content = File::get($envPath);

        $content = preg_replace(
            '/^BROADCAST_(DRIVER|CONNECTION)=.*/m',
            'BROADCAST_CONNECTION=pogo',
            $content
        );

        $vars = [
            'WS_APP_ID' => 'pogo-app',
            'VITE_POGO_WS_PORT' => '80',
            'VITE_POGO_WSS_PORT' => '443',
        ];

        foreach ($vars as $key => $val) {
            if (!str_contains($content, $key . '=')) {
                $content .= PHP_EOL . "$key=$val";
            }
        }

        $content = preg_replace('/^PUSHER_.*\n/m', '', $content);
        $content = preg_replace('/^VITE_PUSHER_.*\n/m', '', $content);

        File::put($envPath, trim($content) . PHP_EOL);
        $this->components->info('.env updated and cleaned.');
    }

    protected function updateFrontend()
    {
        $echoPath = resource_path('js/echo.js');
        $stubContent = file_get_contents(__DIR__ . '/../../stubs/echo.js');

        if (File::exists($echoPath)) {
            File::put($echoPath, $stubContent);
            $this->components->info('resources/js/echo.js updated.');
        }
    }
}
