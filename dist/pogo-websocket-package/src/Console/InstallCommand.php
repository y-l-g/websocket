<?php

namespace Pogo\WebSocket\Console;

use Illuminate\Console\Command;

class InstallCommand extends Command
{
    protected $signature = 'pogo:ws-install';
    protected $description = 'Install the FrankenPHP WebSocket worker script';

    public function handle()
    {
        $stub = __DIR__ . '/../../stubs/worker.php';
        $target = base_path('frankenphp-worker.php');

        if (file_exists($target) && !$this->confirm('frankenphp-worker.php already exists. Overwrite?', false)) {
            return;
        }

        copy($stub, $target);
        $this->info('Worker script published to: ' . $target);
        $this->comment('Ensure your Caddyfile points auth_script to ./frankenphp-worker.php');

        // Optional: Publish Config if we had one
        // $this->call('vendor:publish', ['--tag' => 'pogo-websocket-config']);
    }
}