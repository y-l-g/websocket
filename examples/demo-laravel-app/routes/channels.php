<?php

use Illuminate\Support\Facades\Broadcast;

Broadcast::channel('room.{id}', function ($user, $id) {
    // HACK: Append a random number so every tab looks like a NEW user
    $fakeId = $user->id . '-' . rand(100, 999);

    return [
        'id' => $fakeId,
        'name' => $user->name . ' (' . $fakeId . ')'
    ];
});