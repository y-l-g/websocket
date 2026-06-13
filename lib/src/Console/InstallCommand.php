<?php

namespace Pogo\WebSocket\Console;

use Illuminate\Console\Command;
use Illuminate\Filesystem\Filesystem;
use Illuminate\Support\Env;

class InstallCommand extends Command
{
    protected $signature = 'pogo:ws-install {--force : Overwrite existing configuration}';

    protected $description = 'Install Pogo WebSocket engine';

    public function handle(): int
    {
        $this->components->info('Installing Pogo WebSocket native broadcaster...');

        $this->publishConfiguration();
        $this->installChannelsRoutes();
        $this->enableBroadcasting();
        $this->configureBroadcastingDriver();
        $this->updateEnvironmentFile();
        $this->installFrontendScaffolding();

        $this->newLine();
        $this->components->info('Installation complete.');
        $this->components->warn('Next steps:');
        $this->components->bulletList([
            'Install frontend dependencies if needed: [npm install --save-dev laravel-echo pusher-js].',
            'Run [npm run build] to compile the frontend.',
            'Restart FrankenPHP/Caddy with matching REVERB_APP_ID, REVERB_APP_KEY, and REVERB_APP_SECRET.',
            'Use ShouldBroadcastNow or same-process broadcasting for the native publish path.',
        ]);

        return self::SUCCESS;
    }

    protected function publishConfiguration(): void
    {
        $this->call('config:publish', ['name' => 'broadcasting']);
    }

    protected function installChannelsRoutes(): void
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

    protected function enableBroadcasting(): void
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

    protected function configureBroadcastingDriver(): void
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
                        'key' => env('REVERB_APP_KEY'),
                        'secret' => env('REVERB_APP_SECRET'),
                        'app_id' => env('REVERB_APP_ID'),
                    ],

            CONFIG;

        $updatedContent = preg_replace(
            "/'connections' => \[\n/",
            "'connections' => [\n" . $driverConfig,
            $content
        );
        if ($updatedContent === null) {
            return;
        }

        $filesystem->put($configPath, $updatedContent);
    }

    protected function updateEnvironmentFile(): void
    {
        $envPath = $this->laravel->basePath('.env');

        if (!file_exists($envPath)) {
            return;
        }

        $appId = $this->envString('REVERB_APP_ID', 'pogo-app');
        $appKey = $this->envString('REVERB_APP_KEY', bin2hex(random_bytes(16)));
        $appSecret = $this->envString('REVERB_APP_SECRET', bin2hex(random_bytes(32)));
        $host = $this->envString('REVERB_HOST', 'localhost');
        $port = $this->envString('REVERB_PORT', '8080');
        $scheme = $this->envString('REVERB_SCHEME', 'http');

        Env::writeVariables([
            'BROADCAST_CONNECTION' => 'pogo',
            'REVERB_APP_ID' => $appId,
            'REVERB_APP_KEY' => $appKey,
            'REVERB_APP_SECRET' => $appSecret,
            'REVERB_HOST' => $host,
            'REVERB_PORT' => $port,
            'REVERB_SCHEME' => $scheme,
            'VITE_REVERB_APP_KEY' => $appKey,
            'VITE_REVERB_HOST' => $host,
            'VITE_REVERB_PORT' => $port,
            'VITE_REVERB_SCHEME' => $scheme,
        ], $envPath);
    }

    protected function envString(string $key, string $default): string
    {
        $value = Env::get($key);

        if (is_string($value) && $value !== '') {
            return $value;
        }

        return $default;
    }

    protected function installFrontendScaffolding(): void
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
                    broadcaster: 'reverb',
                    key: import.meta.env.VITE_REVERB_APP_KEY,
                    wsHost: import.meta.env.VITE_REVERB_HOST || window.location.hostname,
                    wsPort: import.meta.env.VITE_REVERB_PORT || 80,
                    wssPort: import.meta.env.VITE_REVERB_PORT || 443,
                    forceTLS: (import.meta.env.VITE_REVERB_SCHEME || 'https') === 'https',
                    disableStats: true,
                    enabledTransports: ['ws', 'wss'],
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
}
