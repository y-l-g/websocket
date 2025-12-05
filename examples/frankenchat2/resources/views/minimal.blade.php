<!DOCTYPE html>
<html>

<head>
    <title>Minimal Debug</title>
    <!-- On charge uniquement notre script de debug isolé -->
    @vite(['resources/js/minimal.js'])
</head>

<body style="font-family: sans-serif; padding: 20px;">

    <h2>⚡ Minimalist Whisper Test</h2>
    <div>Status: <b id="status" style="color: orange">Connecting...</b></div>
    <hr>

    <button id="btn" onclick="sendPing()" disabled>
        Envoyer Whisper (client-ping)
    </button>

    <div id="logs"
        style="margin-top: 20px; border: 1px solid #ccc; padding: 10px; height: 300px; overflow: auto; background: #f9f9f9;">
    </div>

    <script>
        // On passe l'ID user globalement pour que le JS le récupère
        window.userId = {{ auth()->id() ?? 'null' }};
    </script>
</body>

</html>