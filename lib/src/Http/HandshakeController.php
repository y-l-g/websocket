<?php

namespace Pogo\WebSocket\Http;

use Illuminate\Http\Request;
use Illuminate\Routing\Controller;
use Illuminate\Support\Facades\Broadcast;

class HandshakeController extends Controller
{
    public function __invoke(Request $request): mixed
    {
        return Broadcast::auth($request);
    }
}
