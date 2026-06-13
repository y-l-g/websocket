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
    public function configureBroadcastingDriverForTest(): void
    {
        $this->configureBroadcastingDriver();
    }

    public function updateEnvironmentFileForTest(): void
    {
        $this->updateEnvironmentFile();
    }
}
