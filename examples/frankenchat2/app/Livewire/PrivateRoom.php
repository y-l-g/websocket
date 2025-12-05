<?php

namespace App\Livewire;

use App\Events\PrivateMessage;
use Illuminate\Support\Facades\Auth;
use Livewire\Component;

class PrivateRoom extends Component
{
    public int $roomId = 1;
    public string $message = '';

    public function sendMessage()
    {
        if (empty($this->message))
            return;

        broadcast(new PrivateMessage(
            Auth::user()->name,
            $this->message,
            $this->roomId
        ));

        $this->message = '';
    }

    public function render()
    {
        return view('livewire.private-room');
    }
}