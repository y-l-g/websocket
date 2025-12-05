<?php

namespace App\Livewire;

use App\Events\LobbyMessage;
use Illuminate\Support\Facades\Auth;
use Livewire\Component;

class Lobby extends Component
{
    public string $message = '';
    public array $chatHistory = [];

    public function sendMessage()
    {
        if (empty($this->message)) {
            return;
        }

        $user = Auth::user();

        broadcast(new LobbyMessage($user->name, $this->message));

        $this->message = '';
    }

    public function render()
    {
        return view('livewire.lobby');
    }
}