<?php

use Illuminate\Http\Request;
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

Route::middleware(['auth'])->group(function () {
    Volt::route('/stress-test', 'franken-console');
});

Route::post('/webhook', function (Request $request) {
    $secret = 'super-secret-key';
    $signature = $request->header('X-Pusher-Signature');
    $body = $request->getContent();

    $expectedSignature = hash_hmac('sha256', $body, $secret);

    if ($signature !== $expectedSignature) {
        Log::warning("⚠️ Webhook signature mismatch! Recu: $signature / Attendu: $expectedSignature");
        abort(401, 'Invalid signature');
    }

    $payload = json_decode($body, true);

    Log::info("🔔 WEBHOOK REÇU (" . count($payload['events']) . " events) :");

    foreach ($payload['events'] as $event) {
        $channel = $event['channel'];
        $type = $event['name'];

        if ($type === 'channel_occupied') {
            Log::info(" -> 🟢 OCCUPIED: Le canal [$channel] est actif (1er utilisateur).");
        } elseif ($type === 'channel_vacated') {
            Log::info(" -> 🔴 VACATED: Le canal [$channel] est vide (Dernier utilisateur parti).");
        } else {
            Log::info(" -> Event inconnu: $type sur $channel");
        }
    }

    return response()->json(['status' => 'ok']);
});
