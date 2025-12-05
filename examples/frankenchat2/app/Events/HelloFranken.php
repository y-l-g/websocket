<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Broadcasting\PrivateChannel; // On teste un canal PRIVE pour valider l'auth
use Illuminate\Contracts\Broadcasting\ShouldBroadcast;
use Illuminate\Foundation\Events\Dispatchable;
use Illuminate\Queue\SerializesModels;
use Illuminate\Support\Facades\Log;

class HelloFranken implements ShouldBroadcast
{
    use Dispatchable, InteractsWithSockets, SerializesModels;

    public $message;

    public function __construct(string $message)
    {
        $this->message = $message;
        // Log pour debug côté PHP
        Log::info("Construction de l'événement HelloFranken: " . $message);
    }

    public function broadcastOn(): array
    {
        // On utilise un canal privé pour forcer le passage par le Worker d'Auth
        // Assurez-vous que l'utilisateur est connecté (ID 1 pour le test)
        return [
            new PrivateChannel('user.1'),
        ];
    }
}