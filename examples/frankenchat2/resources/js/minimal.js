import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

window.Pusher = Pusher;

// Log utilitaire
function log(msg) {
    const logs = document.getElementById('logs');
    if (logs) {
        const div = document.createElement('div');
        div.innerText = `[${new Date().toLocaleTimeString()}] ${msg}`;
        logs.prepend(div);
    }
    console.log('[DEBUG]', msg);
}

// On attend que le DOM soit chargé
document.addEventListener('DOMContentLoaded', () => {
    log('Initialisation Echo (Script Isolé)...');

    const echo = new Echo({
        broadcaster: 'pusher',
        key: 'frankenphp-key',
        wsHost: window.location.hostname,
        wsPort: 8000,
        wssPort: 443,
        forceTLS: false,
        disableStats: true,
        cluster: 'mt1',
        enabledTransports: ['ws', 'wss'],
        
        // // --- BYPASS AUTHORIZER ---
        // authorizer: (channel, options) => {
        //     return {
        //         authorize: (socketId, callback) => {
        //             log(`🔐 Autorisation locale pour: ${channel.name}`);
        //             callback(false, { auth: "frankenphp:dummy" });
        //         }
        //     };
        // }
    });

    // Debug global Pusher
    echo.connector.pusher.connection.bind('state_change', (states) => {
        log(`État connexion: ${states.current}`);
        const status = document.getElementById('status');
        if (status && states.current === 'connected') {
            status.innerText = 'Connecté (Socket Ouvert)';
            status.style.color = 'blue';
        }
    });

    // Abonnement au canal PRIVE 'test'
    const channel = echo.private('test');

    channel.on('pusher:subscription_succeeded', () => {
        log('✅ SOUSCRIPTION RÉUSSIE !');
        document.getElementById('status').innerText = 'Authentifié & Prêt';
        document.getElementById('status').style.color = 'green';
        document.getElementById('btn').disabled = false;
    });

    channel.on('pusher:subscription_error', (status) => {
        log('❌ ERREUR SOUSCRIPTION: ' + JSON.stringify(status));
        document.getElementById('status').innerText = 'Refusé par le serveur';
        document.getElementById('status').style.color = 'red';
    });

    // Écoute du Whisper
    channel.listenForWhisper('ping', (e) => {
        log(`📩 WHISPER REÇU ! Message: "${e.message}"`);
        alert(`Whisper Reçu: ${e.message}`);
    });

    // Gestion du bouton
    window.sendPing = function() {
        log('📤 Envoi du Whisper...');
        channel.whisper('ping', {
            message: 'Hello depuis ' + (window.userId || 'Guest')
        });
    };
});