<?php

namespace Pogo\WebSocket\Http;

use Illuminate\Http\Request;
use Illuminate\Routing\Controller;
use Illuminate\Support\Facades\Broadcast;
use Pogo\WebSocket\Broadcaster;
use Symfony\Component\HttpKernel\Exception\AccessDeniedHttpException;

class UserAuthController extends Controller
{
    public function __invoke(Request $request): mixed
    {
        $broadcaster = Broadcast::driver();

        if (!$broadcaster instanceof Broadcaster) {
            throw new AccessDeniedHttpException('Pogo WebSocket driver not configured.');
        }

        return $broadcaster->authenticateUser($request);
    }
}
