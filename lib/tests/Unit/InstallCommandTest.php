<?php

namespace Pogo\WebSocket\Tests\Unit;

use Illuminate\Foundation\Application;
use PHPUnit\Framework\TestCase;
use Pogo\WebSocket\Console\InstallCommand;

class InstallCommandTest extends TestCase
{
    private string $tempPath;

    protected function setUp(): void
    {
        parent::setUp();

        $this->tempPath = sys_get_temp_dir() . '/pogo-ws-' . bin2hex(random_bytes(6));
        mkdir($this->tempPath . '/config', 0o755, true);
    }

    protected function tearDown(): void
    {
        $this->removeDirectory($this->tempPath);

        parent::tearDown();
    }

    public function testInstallerConfiguresNativePogoBroadcastConnection(): void
    {
        file_put_contents($this->tempPath . '/config/broadcasting.php', <<<'PHP'
            <?php

            return [
                'default' => env('BROADCAST_CONNECTION', 'null'),
                'connections' => [
                    'log' => [
                        'driver' => 'log',
                    ],
                ],
            ];
            PHP);

        $command = $this->newCommand();
        $command->configureBroadcastingDriverForTest();

        $content = file_get_contents($this->tempPath . '/config/broadcasting.php');

        $this->assertIsString($content);
        $this->assertStringContainsString("'pogo' => [", $content);
        $this->assertStringContainsString("'driver' => 'pogo'", $content);
        $this->assertStringContainsString("'key' => env('REVERB_APP_KEY')", $content);
        $this->assertStringNotContainsString("'driver' => 'reverb'", $content);
    }

    public function testInstallerSetsNativeBroadcastConnectionInEnvironment(): void
    {
        file_put_contents($this->tempPath . '/.env', "APP_NAME=Laravel\n");

        $command = $this->newCommand();
        $command->updateEnvironmentFileForTest();

        $content = file_get_contents($this->tempPath . '/.env');

        $this->assertIsString($content);
        $this->assertStringContainsString('BROADCAST_CONNECTION=pogo', $content);
        $this->assertMatchesRegularExpression('/^REVERB_APP_ID="?pogo-app"?$/m', $content);
        $this->assertStringContainsString('VITE_REVERB_APP_KEY=', $content);
    }

    public function testInstallerEnablesBroadcastingInLaravelElevenBootstrap(): void
    {
        mkdir($this->tempPath . '/bootstrap', 0o755, true);
        file_put_contents($this->tempPath . '/bootstrap/app.php', <<<'PHP'
            <?php

            use Illuminate\Foundation\Application;
            use Illuminate\Foundation\Configuration\Exceptions;
            use Illuminate\Foundation\Configuration\Middleware;

            return Application::configure(basePath: dirname(__DIR__))
                ->withRouting(
                    web: __DIR__.'/../routes/web.php',
                    commands: __DIR__.'/../routes/console.php',
                    health: '/up',
                )
                ->withMiddleware(function (Middleware $middleware): void {
                    //
                })
                ->withExceptions(function (Exceptions $exceptions): void {
                    //
                })->create();
            PHP);

        $command = $this->newCommand();
        $command->enableBroadcastingForTest();

        $content = file_get_contents($this->tempPath . '/bootstrap/app.php');

        $this->assertIsString($content);
        $this->assertStringContainsString("channels: __DIR__.'/../routes/channels.php',", $content);
    }

    public function testInstallerDoesNotDuplicateExistingBroadcastingRoutes(): void
    {
        mkdir($this->tempPath . '/bootstrap', 0o755, true);
        file_put_contents($this->tempPath . '/bootstrap/app.php', <<<'PHP'
            <?php

            return Illuminate\Foundation\Application::configure(basePath: dirname(__DIR__))
                ->withRouting(
                    web: __DIR__.'/../routes/web.php',
                    channels: __DIR__.'/../routes/channels.php',
                    commands: __DIR__.'/../routes/console.php',
                )->create();
            PHP);

        $command = $this->newCommand();
        $command->enableBroadcastingForTest();
        $command->enableBroadcastingForTest();

        $content = file_get_contents($this->tempPath . '/bootstrap/app.php');

        $this->assertIsString($content);
        $this->assertSame(1, substr_count($content, "channels: __DIR__.'/../routes/channels.php',"));
    }

    public function testInstallerEnablesBroadcastingNextToExistingCommandsRoute(): void
    {
        mkdir($this->tempPath . '/bootstrap', 0o755, true);
        file_put_contents($this->tempPath . '/bootstrap/app.php', <<<'PHP'
            <?php

            return Illuminate\Foundation\Application::configure(basePath: dirname(__DIR__))
                ->withRouting(
                    web: __DIR__.'/../routes/web.php',
                    commands: __DIR__.'/../routes/console.php',
                )->create();
            PHP);

        $command = $this->newCommand();
        $command->enableBroadcastingForTest();

        $content = file_get_contents($this->tempPath . '/bootstrap/app.php');

        $this->assertIsString($content);
        $this->assertStringContainsString(
            "commands: __DIR__.'/../routes/console.php'," . PHP_EOL . "        channels: __DIR__.'/../routes/channels.php',",
            $content,
        );
    }

    public function testInstallerCreatesEchoScriptAndImportsItOnce(): void
    {
        mkdir($this->tempPath . '/resources/js', 0o755, true);
        file_put_contents($this->tempPath . '/resources/js/app.js', "import './bootstrap';" . PHP_EOL);

        $command = $this->newCommand();
        $command->installFrontendScaffoldingForTest();
        $command->installFrontendScaffoldingForTest();

        $echoContent = file_get_contents($this->tempPath . '/resources/js/echo.js');
        $appContent = file_get_contents($this->tempPath . '/resources/js/app.js');

        $this->assertIsString($echoContent);
        $this->assertIsString($appContent);
        $this->assertStringContainsString("import Echo from 'laravel-echo';", $echoContent);
        $this->assertSame(1, substr_count($appContent, "import './echo';"));
    }

    public function testInstallerPreservesBootstrapBeforeAppEntryPreference(): void
    {
        mkdir($this->tempPath . '/resources/js', 0o755, true);
        file_put_contents($this->tempPath . '/resources/js/bootstrap.js', "window.axios = {};" . PHP_EOL);
        file_put_contents($this->tempPath . '/resources/js/app.js', "import './bootstrap';" . PHP_EOL);

        $command = $this->newCommand();
        $command->installFrontendScaffoldingForTest();

        $bootstrapContent = file_get_contents($this->tempPath . '/resources/js/bootstrap.js');
        $appContent = file_get_contents($this->tempPath . '/resources/js/app.js');

        $this->assertIsString($bootstrapContent);
        $this->assertIsString($appContent);
        $this->assertSame(1, substr_count($bootstrapContent, "import './echo';"));
        $this->assertSame(0, substr_count($appContent, "import './echo';"));
    }

    private function newCommand(): TestableInstallCommand
    {
        $command = new TestableInstallCommand();
        $command->setLaravel(new Application($this->tempPath));

        return $command;
    }

    private function removeDirectory(string $path): void
    {
        if (!is_dir($path)) {
            return;
        }

        $items = scandir($path);
        if ($items === false) {
            return;
        }

        foreach ($items as $item) {
            if ($item === '.' || $item === '..') {
                continue;
            }

            $child = $path . '/' . $item;
            if (is_dir($child)) {
                $this->removeDirectory($child);
            } else {
                unlink($child);
            }
        }

        rmdir($path);
    }
}

class TestableInstallCommand extends InstallCommand
{
    public function enableBroadcastingForTest(): void
    {
        $this->enableBroadcasting();
    }

    public function configureBroadcastingDriverForTest(): void
    {
        $this->configureBroadcastingDriver();
    }

    public function updateEnvironmentFileForTest(): void
    {
        $this->updateEnvironmentFile();
    }

    public function installFrontendScaffoldingForTest(): void
    {
        $this->ensureConsoleComponentsForTest();
        $this->installFrontendScaffolding();
    }

    public function option($key = null)
    {
        if ($key === 'force') {
            return false;
        }

        return parent::option($key);
    }

    private function ensureConsoleComponentsForTest(): void
    {
        $input = new \Symfony\Component\Console\Input\ArrayInput([]);
        $output = new \Illuminate\Console\OutputStyle(
            $input,
            new \Symfony\Component\Console\Output\BufferedOutput(),
        );

        $this->setOutput($output);
        $this->components = new \Illuminate\Console\View\Components\Factory($output);
    }
}
