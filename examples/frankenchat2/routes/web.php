<?php

use App\Events\HelloFranken;
use Illuminate\Support\Facades\Route;
use Laravel\Fortify\Features;
use Livewire\Volt\Volt;

Route::get('/', function () {
    return view('welcome');
})->name('home');

Route::view('dashboard', 'dashboard')
    ->middleware(['auth', 'verified'])
    ->name('dashboard');

Route::middleware(['auth'])->group(function () {
    Route::redirect('settings', 'settings/profile');

    Volt::route('settings/profile', 'settings.profile')->name('profile.edit');
    Volt::route('settings/password', 'settings.password')->name('user-password.edit');
    Volt::route('settings/appearance', 'settings.appearance')->name('appearance.edit');

    Volt::route('settings/two-factor', 'settings.two-factor')
        ->middleware(
            when(
                Features::canManageTwoFactorAuthentication()
                && Features::optionEnabled(Features::twoFactorAuthentication(), 'confirmPassword'),
                ['password.confirm'],
                [],
            ),
        )
        ->name('two-factor.show');
});

Route::get('/echo-test', function () {
    // On force le login de l'user 1 pour le test (si vous avez des users en base)
    // Sinon, connectez-vous normalement via votre interface.
    if (!auth()->check()) {
        return "Veuillez vous connecter d'abord.";
    }

    return view('echo-test');
});

Route::get('/fire-event', function () {
    event(new HelloFranken("Message envoyé depuis FrankenPHP à " . now()));
    return "Événement envoyé !";
});

Route::get('/presence-debug', function () {
    if (!auth()->check()) {
        return "ERREUR: Connectez-vous d'abord !";
    }
    return view('presence-debug');
});

Route::get('/minimal', function () {
    if (!auth()->check()) {
        return "ERREUR: Il faut être connecté (login) pour tester un canal privé.";
    }
    return view('minimal');
});

Route::get('/test-presence', function () {
    if (!auth()->check())
        return "Loggez-vous !";
    return view('test-presence');
});

Route::get('/debug-events', function () {
    if (!auth()->check())
        return "Loggez-vous !";
    return view('debug-events');
});