<?php

namespace Pogo\WebSocket\Http;

use Illuminate\Http\Request;
use Illuminate\Routing\Controller;
use Illuminate\Support\Facades\Broadcast;

class HandshakeController extends Controller
{
    public function __invoke(Request $request)
    {
        // 1. Force the broadcaster driver (optional, ensures we use ours)
        // But usually Broadcast::auth() uses the default driver from config.

        // 2. Authenticate
        // This triggers Broadcaster::auth -> verifyUserCanAccessChannel -> routes/channels.php
        return Broadcast::auth($request);
    }
}