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
        $this->appId = $config['app_id'] ?? 'frankenphp-app';
    }

    public function auth($request)
    {
        $channelName = $this->normalizeChannelName($request->channel_name);

        try {
            $result = $this->verifyUserCanAccessChannel($request, $channelName);
        } catch (AccessDeniedHttpException $e) {
            throw $e;
        }

        $response = [
            'auth' => $this->appId . ':dummy_signature_for_client',
        ];

        if ($request->channel_name && str_starts_with($request->channel_name, 'presence-')) {
            $channelData = [
                'user_id' => (string) ($result['id'] ?? $request->user()->getAuthIdentifier()),
                'user_info' => $result,
            ];

            $response['channel_data'] = json_encode($channelData);
        }

        return $response;
    }

    public function validAuthenticationResponse($request, $result)
    {
        return $result;
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