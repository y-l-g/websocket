<?php

use Illuminate\Support\Facades\Broadcast;

Broadcast::channel('user.{id}', function ($user, $id) {
    return (int) $user->id === (int) $id;
});

Broadcast::channel('room.{roomId}', function ($user, $roomId) {
    \Illuminate\Support\Facades\Log::info("User {$user->id} attempting to join room {$roomId}");
    return is_numeric($roomId);
});

Broadcast::channel('war-room', function ($user) {
    return [
        'id' => $user->id,
        'name' => $user->name,
        'color' => '#' . substr(md5($user->name), 0, 6)
    ];
});

Broadcast::channel('debug', function ($user) {
    Log::info("Succès ! User {$user->id} a rejoint le canal debug.");
    return ['id' => $user->id, 'name' => $user->name];
});

Broadcast::channel('test', function ($user) {
    // Si cette ligne est exécutée, c'est que l'auth a réussi
    return true;
});