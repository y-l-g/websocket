<!DOCTYPE html>
<html lang="fr">

<head>
    <meta charset="UTF-8">
    <title>FrankenPHP Presence Debugger</title>
    @vite(['resources/css/app.css', 'resources/js/app.js'])
    <style>
        body {
            font-family: monospace;
            padding: 20px;
            background: #f0f0f0;
        }

        .box {
            background: white;
            border: 1px solid #ccc;
            padding: 15px;
            margin-bottom: 20px;
            border-radius: 5px;
        }

        .log-entry {
            border-bottom: 1px solid #eee;
            padding: 4px 0;
            font-size: 12px;
        }

        .success {
            color: green;
            font-weight: bold;
        }

        .error {
            color: red;
            font-weight: bold;
        }

        .info {
            color: blue;
        }

        .whisper {
            color: purple;
            font-weight: bold;
        }
    </style>
</head>

<body>
    <div class="box">
        <h2 class="text-xl font-bold mb-2">🕵️ Presence Debugger</h2>
        <div>User ID: <strong>{{ auth()->id() }}</strong></div>
        <div>Canal Laravel: <strong>debug</strong> (Réseau: presence-debug)</div>
        <div class="mt-2">
            État Echo: <span id="status" class="error">Déconnecté</span>
        </div>
        <div class="mt-4">
            <button onclick="sendWhisper()" class="bg-purple-600 text-white px-4 py-2 rounded hover:bg-purple-700">
                🗣️ Envoyer Whisper (Ping)
            </button>
        </div>
    </div>

    <div class="grid grid-cols-2 gap-4">
        <!-- Liste des membres -->
        <div class="box">
            <h3 class="font-bold border-b pb-2 mb-2">👥 Membres Présents (<span id="count">0</span>)</h3>
            <ul id="members-list" class="list-disc pl-5"></ul>
        </div>

        <!-- Logs -->
        <div class="box">
            <h3 class="font-bold border-b pb-2 mb-2">📜 Logs d'événements</h3>
            <div id="logs" class="h-64 overflow-y-auto bg-gray-50 p-2 border"></div>
        </div>
    </div>

    <!-- 1. Définition des variables globales AVANT le chargement des modules -->
    <script>
        window.currentUserId = {{ auth()->id() }};
        window.currentUserName = "{{ auth()->user()->name }}";
    </script>

    <script type="module">
        // Fonctions utilitaires
        const logDiv = document.getElementById('logs');
        const membersList = document.getElementById('members-list');
        const countSpan = document.getElementById('count');

        function log(msg, type = 'normal') {
            const div = document.createElement('div');
            div.className = `log-entry ${type}`;
            div.textContent = `[${new Date().toLocaleTimeString()}] ${msg}`;
            logDiv.prepend(div);
            console.log(msg);
        }

        function updateMembers(members) {
            membersList.innerHTML = '';
            // Conversion en tableau si nécessaire
            const membersArray = Array.isArray(members) ? members : Object.values(members);
            countSpan.innerText = membersArray.length;

            membersArray.forEach(member => {
                const li = document.createElement('li');
                li.textContent = `${member.name || 'Anonyme'} (ID: ${member.id})`;
                if (String(member.id) === String(window.currentUserId)) {
                    li.style.fontWeight = 'bold';
                    li.textContent += ' (Moi)';
                }
                membersList.appendChild(li);
            });
        }

        setTimeout(() => {
            log('Initialisation Echo...', 'info');

            // Configuration Echo avec Bypass
            window.Echo = new Echo({
                broadcaster: 'pusher',
                key: 'frankenphp-key',
                wsHost: window.location.hostname,
                wsPort: 8000,
                wssPort: 443,
                forceTLS: false,
                disableStats: true,
                enabledTransports: ['ws', 'wss'],

                // --- FIX: Authorizer Bypass ---
                // Puisque raw-debug.html prouve que le serveur Go fait bien l'auth,
                // on dit au JS de ne pas s'embêter avec une requête HTTP inutile.
                authorizer: (channel, options) => {
                    return {
                        authorize: (socketId, callback) => {
                            log(`🔐 Autorisation Bypass pour ${channel.name}`, 'info');

                            let authData = {
                                auth: "frankenphp:dummy_signature"
                            };

                            // Données bidons requises par PusherJS pour initier la connexion Presence
                            // Elles seront écrasées par les vraies données du serveur Go une seconde plus tard
                            if (channel.name.startsWith('presence-')) {
                                authData.channel_data = JSON.stringify({
                                    user_id: window.currentUserId,
                                    user_info: { name: 'Connexion...' }
                                });
                            }

                            callback(false, authData);
                        }
                    };
                },
            });

            // --- CONNEXION ---
            // On utilise 'debug' car Echo ajoute automatiquement 'presence-'
            // Donc 'debug' -> 'presence-debug'
            window.Echo.join('debug')
                .here((users) => {
                    log(`✅ HERE: Connecté avec succès !`, 'success');
                    updateMembers(users);

                    document.getElementById('status').innerText = 'Connecté & Synchronisé';
                    document.getElementById('status').className = 'success';
                })
                .joining((user) => {
                    log(`➕ JOINING: ${user.name} (${user.id})`, 'info');
                    // On triche un peu pour l'affichage rapide sans gérer l'état complet
                    const li = document.createElement('li');
                    li.textContent = `${user.name} (ID: ${user.id})`;
                    membersList.appendChild(li);
                    countSpan.innerText = parseInt(countSpan.innerText) + 1;
                })
                .leaving((user) => {
                    log(`➖ LEAVING: ${user.name} (${user.id})`, 'error');
                    countSpan.innerText = Math.max(0, parseInt(countSpan.innerText) - 1);
                })
                .listenForWhisper('ping', (e) => {
                    log(`🗣️ WHISPER REÇU de ${e.name}`, 'whisper');
                });

        }, 500);

        // Fonction globale pour le bouton
        window.sendWhisper = function () {
            log('Envoi Whisper...', 'info');
            window.Echo.join('debug')
                .whisper('ping', {
                    name: window.currentUserName,
                    time: new Date().toISOString()
                });
        };
    </script>
</body>

</html>