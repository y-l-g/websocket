<?php

namespace Pogo\WebSocket;

use Illuminate\Broadcasting\Broadcasters\Broadcaster as BaseBroadcaster;
use Illuminate\Broadcasting\Broadcasters\UsePusherChannelConventions;
use Symfony\Component\HttpKernel\Exception\AccessDeniedHttpException;

class Broadcaster extends BaseBroadcaster
{
    use UsePusherChannelConventions;

    protected $appId;

    public function __construct(array $config = [])
    {
        // 1. Try config
        $this->appId = $config['app_id'] ?? null;

        // 2. Fallback: Generate same hash as Caddy if using default auth path
        // This matches the auto-generation logic in caddy.go
        if (!$this->appId) {
            $path = $config['options']['auth_path'] ?? '/broadcasting/auth';
            $this->appId = md5($path);
        }
    }

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
            frankenphp_websocket_publish($this->appId, $channel, $event, $payloadJson);
        }
    }
}