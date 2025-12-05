<!DOCTYPE html>
<html>

<head>
    <title>Test Echo & FrankenPHP</title>
    @vite(['resources/css/app.css', 'resources/js/app.js'])
</head>

<body class="p-10">
    <h1 class="text-xl font-bold mb-4">FrankenPHP WebSocket Test</h1>

    <div class="mb-4">
        Statut: <span id="status" class="font-bold text-orange-500">Connexion...</span>
    </div>

    <div class="border p-4 bg-gray-100 min-h-[200px]" id="messages">
        <!-- Les messages apparaîtront ici -->
    </div>

    <button onclick="fireEvent()" class="mt-4 px-4 py-2 bg-blue-500 text-white rounded">
        Envoyer un Événement (AJAX)
    </button>

    <script type="module">
        const userId = {{ auth()->id() }}; // ID de l'utilisateur connecté

        // Attendre que Echo soit chargé
        setTimeout(() => {
            console.log("Abonnement au canal private-user." + userId);

            window.Echo.private(`user.${userId}`)
                .listen('.App\\Events\\HelloFranken', (e) => {
                    console.log("Événement reçu:", e);
                    const el = document.getElementById('messages');
                    el.innerHTML += `<div class="text-green-600">Reçu: ${e.message}</div>`;
                })
                .on('pusher:subscription_succeeded', () => {
                    document.getElementById('status').innerText = 'Connecté & Authentifié ✅';
                    document.getElementById('status').className = 'font-bold text-green-500';
                })
                .error((error) => {
                    console.error("Erreur Echo:", error);
                    document.getElementById('status').innerText = 'Erreur (Voir Console)';
                    document.getElementById('status').className = 'font-bold text-red-500';
                });
        }, 500); // Petit délai pour s'assurer que Vite a chargé Echo
    </script>

    <script>
        function fireEvent() {
            fetch('/fire-event');
        }
    </script>
</body>

</html>