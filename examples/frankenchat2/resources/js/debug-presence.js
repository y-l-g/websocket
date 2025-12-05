import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

window.Pusher = Pusher;

function log(msg) {
    const el = document.getElementById('logs');
    if (el) el.innerHTML = `<div>[${new Date().toLocaleTimeString()}] ${msg}</div>` + el.innerHTML;
    console.log('[PRESENCE-DEBUG]', msg);
}

document.addEventListener('DOMContentLoaded', () => {
    // Récupération de l'ID injecté dans la vue
    const userId = window.currentUserId; 

    log('🚀 Initialisation du test Presence...');

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
        
        // --- LE FIX EST ICI ---
        authorizer: (channel, options) => {
            return {
                authorize: (socketId, callback) => {
                    log(`🔐 Tentative d'auth locale pour ${channel.name}`);

                    let authData = { 
                        auth: "frankenphp:dummy_signature" 
                    };

                    if (channel.name.startsWith('presence-')) {
                        authData.channel_data = JSON.stringify({
                            user_id: userId, 
                            user_info: { name: 'Connexion...' } 
                        });
                    }

                    callback(false, authData);
                }
            };
        }
    });

    log('📡 Connexion à presence-war-room (alias war-room)...');

    // .join ajoute automatiquement le préfixe 'presence-'
    echo.join('war-room')
        .here((users) => {
            log(`✅ HERE REÇU ! ${users.length} utilisateurs en ligne.`);
            console.log('Données brutes HERE:', users);
            document.getElementById('status').style.color = 'green';
            document.getElementById('status').innerText = 'SUCCÈS - VOIR LOGS';
        })
        .joining((user) => {
            log(`➕ JOINING: ${user.name}`);
        })
        .leaving((user) => {
            log(`➖ LEAVING: ${user.name}`);
        })
        .error((err) => {
            log(`❌ ERREUR: ${JSON.stringify(err)}`);
        });
});