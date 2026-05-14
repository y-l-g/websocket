<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Contracts\Broadcasting\ShouldBroadcastNow;
use Illuminate\Foundation\Events\Dispatchable;
use Illuminate\Queue\SerializesModels;

class BenchEvent implements ShouldBroadcastNow
{
    use Dispatchable;
    use InteractsWithSockets;
    use SerializesModels;

    public $payload;
    public $sentAt;

    public function __construct(public int $id, public int $size = 100)
    {
        $this->sentAt = microtime(true) * 1000; // Milliseconds
        $this->payload = str_repeat('X', $size); // Generate load
    }

    public function broadcastOn()
    {
        return new Channel('bench-channel');
    }

    public function broadcastAs()
    {
        return 'bench.event';
    }
}
