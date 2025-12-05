<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Contracts\Broadcasting\ShouldBroadcastNow; // Important: Now
use Illuminate\Foundation\Events\Dispatchable;
use Illuminate\Queue\SerializesModels;

class LobbyMessage implements ShouldBroadcastNow
{
    use Dispatchable, InteractsWithSockets, SerializesModels;

    public string $username;
    public string $message;
    public string $time;

    public function __construct(string $username, string $message)
    {
        $this->username = $username;
        $this->message = $message;
        $this->time = now()->format('H:i:s');
    }

    public function broadcastOn(): array
    {
        return [
            new Channel('public-lobby'),
        ];
    }

    // AJOUTE CECI : On force le nom de l'événement pour le JS
    public function broadcastAs(): string
    {
        return 'lobby-message';
    }
}