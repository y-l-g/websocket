<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Broadcasting\PresenceChannel;
use Illuminate\Broadcasting\PrivateChannel;
use Illuminate\Contracts\Broadcasting\ShouldBroadcastNow;
use Illuminate\Foundation\Events\Dispatchable;
use Illuminate\Queue\SerializesModels;

class PrivateMessage implements ShouldBroadcastNow
{
    use Dispatchable, InteractsWithSockets, SerializesModels;

    public string $username;
    public string $message;
    public int $roomId;

    public function __construct(string $username, string $message, int $roomId)
    {
        $this->username = $username;
        $this->message = $message;
        $this->roomId = $roomId;
    }

    public function broadcastOn(): array
    {
        return [
            new PrivateChannel('room.' . $this->roomId),
        ];
    }

    public function broadcastAs(): string
    {
        return 'private-message';
    }
}