<?php

namespace Pogo\WebSocket\Console;

use Illuminate\Console\Command;
use Illuminate\Filesystem\Filesystem;
use Illuminate\Support\Env;
use Illuminate\Support\Facades\Process;

use function Laravel\Prompts\confirm;

class InstallCommand extends Command
{
    protected $signature = 'pogo:ws-install {--force : Overwrite existing configuration}';

    protected $description = 'Install Pogo WebSocket engine';

    public function handle()
    {
        $this->components->info('🐘 Installing Pogo WebSocket Engine...');

        $this->publishConfiguration();
        $this->installChannelsRoutes();
        $this->enableBroadcasting();
        $this->configureBroadcastingDriver();
        $this->updateEnvironmentFile();
        $this->installWebSocketWorker(); // <-- Ajouté ici
        $this->installFrontendScaffolding();
        $this->installNodeDependencies();

        $this->newLine();
        $this->components->info('✅ Installation complete!');
        $this->components->warn('Next steps:');
        $this->components->bulletList([
            'Run [npm run build] to compile the frontend.',
            'Restart FrankenPHP to load the new worker/config.',
        ]);
    }

    protected function publishConfiguration()
    {
        $this->call('config:publish', ['name' => 'broadcasting']);
    }

    protected function installChannelsRoutes()
    {
        $path = $this->laravel->basePath('routes/channels.php');

        if (file_exists($path) && !$this->option('force')) {
            return;
        }

        $stub = <<<'PHP'
            <?php

            use Illuminate\Support\Facades\Broadcast;

            Broadcast::channel('App.Models.User.{id}', function ($user, $id) {
                return (int) $user->id === (int) $id;
            });
            PHP;

        (new Filesystem())->put($path, $stub);
    }

    protected function enableBroadcasting()
    {
        $appBootstrapPath = $this->laravel->bootstrapPath('app.php');
        $filesystem = new Filesystem();

        if (!$filesystem->exists($appBootstrapPath)) {
            return;
        }

        $content = $filesystem->get($appBootstrapPath);

        if (str_contains($content, 'channels: ')) {
            return;
        }

        if (str_contains($content, 'commands: __DIR__.\'/../routes/console.php\',')) {
            $filesystem->replaceInFile(
                'commands: __DIR__.\'/../routes/console.php\',',
                'commands: __DIR__.\'/../routes/console.php\',' . PHP_EOL . '        channels: __DIR__.\'/../routes/channels.php\',',
                $appBootstrapPath,
            );
        } elseif (str_contains($content, '->withRouting(')) {
            $filesystem->replaceInFile(
                '->withRouting(',
                '->withRouting(' . PHP_EOL . '        channels: __DIR__.\'/../routes/channels.php\',',
                $appBootstrapPath,
            );
        }
    }

    protected function configureBroadcastingDriver()
    {
        $configPath = $this->laravel->configPath('broadcasting.php');
        $filesystem = new Filesystem();

        if (!$filesystem->exists($configPath)) {
            return;
        }

        $content = $filesystem->get($configPath);

        if (str_contains($content, "'pogo' => [")) {
            return;
        }

        $driverConfig = <<<'CONFIG'
                    'pogo' => [
                        'driver' => 'pogo',
                        'app_id' => env('WS_APP_ID', 'pogo-app'),
                        'secret' => env('WS_APP_SECRET', 'super-secret-key'),
                    ],

            CONFIG;

        $content = preg_replace(
            "/'connections' => \[\n/",
            "'connections' => [\n" . $driverConfig,
            $content
        );

        $filesystem->put($configPath, $content);
    }

    protected function updateEnvironmentFile()
    {
        $envPath = $this->laravel->basePath('.env');

        if (!file_exists($envPath)) {
            return;
        }

        Env::writeVariables([
            'BROADCAST_CONNECTION' => 'pogo',
            'WS_APP_ID' => 'pogo-app',
            'WS_APP_SECRET' => 'super-secret-key',
            'VITE_POGO_PORT' => '80',
            'VITE_POGO_WSS_PORT' => '443',
        ], $envPath);
    }

    protected function installFrontendScaffolding()
    {
        $filesystem = new Filesystem();
        $resourcePath = $this->laravel->resourcePath('js');

        if (!$filesystem->isDirectory($resourcePath)) {
            $filesystem->makeDirectory($resourcePath, 0o755, true);
        }

        $echoScriptPath = $resourcePath . '/echo.js';

        if (!$filesystem->exists($echoScriptPath) || $this->option('force')) {
            $jsContent = <<<'JS'
                import Echo from 'laravel-echo';
                import Pusher from 'pusher-js';

                window.Pusher = Pusher;

                window.Echo = new Echo({
                    broadcaster: 'pusher',
                    key: 'pogo-key',
                    cluster: 'mt1',
                    wsHost: import.meta.env.VITE_POGO_HOST || window.location.hostname,
                    wsPort: import.meta.env.VITE_POGO_PORT || 80,
                    wssPort: import.meta.env.VITE_POGO_WSS_PORT || 443,
                    forceTLS: false,
                    disableStats: true,
                    enabledTransports: ['ws', 'wss'],
                    userAuthentication: {
                        endpoint: "/pogo/user-auth"
                    }
                });
                JS;
            $filesystem->put($echoScriptPath, $jsContent);
            $this->components->info("Created resources/js/echo.js");
        }

        $filesToCheck = [
            $resourcePath . '/bootstrap.js',
            $resourcePath . '/app.js',
        ];

        foreach ($filesToCheck as $file) {
            if ($filesystem->exists($file)) {
                $content = $filesystem->get($file);
                if (!str_contains($content, './echo')) {
                    $filesystem->append($file, PHP_EOL . "import './echo';" . PHP_EOL);
                    $this->components->info("Updated " . basename($file));
                }
                break;
            }
        }
    }

    protected function installNodeDependencies()
    {
        if (!confirm('Run Laravel broadcasting scaffolding (npm install)?', default: true)) {
            return;
        }

        $packages = 'laravel-echo pusher-js';
        $commands = [];

        if (file_exists(base_path('pnpm-lock.yaml'))) {
            $commands = ["pnpm add -D $packages"];
        } elseif (file_exists(base_path('yarn.lock'))) {
            $commands = ["yarn add -D $packages"];
        } elseif (file_exists(base_path('bun.lockb'))) {
            $commands = ["bun add -D $packages"];
        } else {
            $commands = ["npm install --save-dev $packages"];
        }

        $command = Process::command(implode(' && ', $commands))
            ->path(base_path());

        if (!windows_os()) {
            $command->tty(true);
        }

        if ($command->run()->failed()) {
            $this->components->warn("Node dependency installation failed. Please run: " . implode(' && ', $commands));
        } else {
            $this->components->info('Node dependencies installed successfully.');
        }
    }

    protected function installWebSocketWorker()
    {
        $filesystem = new Filesystem();
        $workerPath = $this->laravel->publicPath('websocket-worker.php');

        if ($filesystem->exists($workerPath) && !$this->option('force')) {
            $this->components->info('WebSocket worker already exists.');
            return;
        }

        $workerContent = <<<'PHP'
            <?php

            // Set a default for the application base path and public path if they are missing...
            $_SERVER['APP_BASE_PATH'] = $_ENV['APP_BASE_PATH'] ?? $_SERVER['APP_BASE_PATH'] ?? __DIR__.'/..';
            $_SERVER['APP_PUBLIC_PATH'] = $_ENV['APP_PUBLIC_PATH'] ?? $_SERVER['APP_BASE_PATH'] ?? __DIR__;

            require __DIR__.'/../vendor/laravel/octane/bin/frankenphp-worker.php';
            PHP;

        $filesystem->put($workerPath, $workerContent);
        $this->components->info('Created websocket-worker.php');
    }
}
