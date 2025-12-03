<?php

use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\Auth;
use App\Events\TestMessage;
use App\Models\User;

// 1. Login Helper (Simulates a real app login)
Route::get('/login-test', function () {
    // Ensure migration is run: php artisan migrate
    $user = User::firstOrCreate(
        ['email' => 'test@example.com'],
        ['name' => 'Test User', 'password' => bcrypt('password')]
    );

    Auth::login($user);

    return "Logged in as ID: " . $user->id;
});

// 2. Client Page (Protected to ensure session cookie exists)
Route::get('/ws-test', function () {
    if (!Auth::check()) {
        return redirect('/login-test');
    }
    return file_get_contents(public_path('ws_test.html'));
});

// 3. Trigger Event
Route::get('/fire', function () {
    if (!Auth::check()) {
        return "Not logged in";
    }

    $msg = 'Hello ' . Auth::user()->name . ' at ' . now();

    // Dispatch standard Laravel Event
    event(new TestMessage(Auth::id(), $msg));

    return "Fired event to channel: private-test." . Auth::id();
});