<!DOCTYPE html>
<html>

<head>
    <title>Presence Nuclear Test</title>
    @vite(['resources/js/debug-presence.js'])
</head>

<body style="font-family: monospace; padding: 20px;">
    <h1>Presence Channel Debugger</h1>
    <h3>Status: <span id="status" style="color:orange">Connexion...</span></h3>

    <div id="logs" style="background: #eee; padding: 10px; border: 1px solid #999; height: 400px; overflow: auto;">
    </div>

    <script>
        // On injecte l'ID pour le script JS
        window.currentUserId = {{ auth()->id() }};
    </script>
</body>

</html>