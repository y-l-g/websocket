<?php

namespace Pogo\WebSocket\Console;

use Illuminate\Console\Command;

class InstallCommand extends Command
{
    protected $signature = 'pogo:ws-install';
    protected $description = 'Setup the FrankenPHP WebSocket worker script';

    public function handle()
    {
        $octanePath = public_path('frankenphp-worker.php');
        $targetPath = public_path('websocket-worker.php');

        if (file_exists($octanePath)) {
            $this->components->info('Laravel Octane detected.');
            $this->components->warn('Skipping standalone worker installation.');
            $this->newLine();
            $this->components->bulletList([
                "Since you have Octane, you should use its optimized worker.",
                "Please update your Caddyfile configuration:",
                "<fg=yellow>auth_script public/frankenphp-worker.php</>"
            ]);
            return;
        }

        if (file_exists($targetPath)) {
            $this->components->warn('The file [public/websocket-worker.php] already exists.');
            $this->newLine();
            $this->components->bulletList([
                "We preserved your existing file.",
                "Ensure your Caddyfile configuration uses it:",
                "<fg=yellow>auth_script public/websocket-worker.php</>"
            ]);
            return;
        }

        $stub = __DIR__ . '/../../stubs/worker.php';

        if (!file_exists($stub)) {
            $this->error('Package Error: Worker stub not found.');
            return;
        }

        copy($stub, $targetPath);

        $this->components->info('Worker installed successfully.');
        $this->newLine();
        $this->components->bulletList([
            "File created at: <fg=gray>public/websocket-worker.php</>",
            "Please update your Caddyfile configuration:",
            "<fg=yellow>auth_script public/websocket-worker.php</>"
        ]);
    }
}