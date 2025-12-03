<?php

namespace Pogo\WebSocket;

use Illuminate\Broadcasting\Broadcasters\Broadcaster as BaseBroadcaster;
use Illuminate\Broadcasting\Broadcasters\UsePusherChannelConventions;
use Symfony\Component\HttpKernel\Exception\AccessDeniedHttpException;

class Broadcaster extends BaseBroadcaster
{
    use UsePusherChannelConventions;

    public function auth($request)
    {
        $channelName = $this->normalizeChannelName($request->channel_name);

        try {
            $result = $this->verifyUserCanAccessChannel($request, $channelName);
        } catch (AccessDeniedHttpException $e) {
            throw $e;
        }

        return $this->validAuthenticationResponse($request, $result);
    }

    public function validAuthenticationResponse($request, $result)
    {
        if (is_bool($result)) {
            return [];
        }

        // Return a flat structure. 
        // 'info' contains the actual data from the channel callback.
        return [
            'user_id' => (string) ($result['id'] ?? null),
            'info' => $result,
        ];
    }

    public function broadcast(array $channels, $event, array $payload = [])
    {
        if (!function_exists('frankenphp_websocket_publish')) {
            return;
        }

        $payloadJson = json_encode($payload);

        foreach ($channels as $channel) {
            frankenphp_websocket_publish($channel, $event, $payloadJson);
        }
    }
}