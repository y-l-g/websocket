<?php

namespace Pogo\WebSocket;

use Illuminate\Broadcasting\Broadcasters\Broadcaster as BaseBroadcaster;
use Illuminate\Broadcasting\Broadcasters\UsePusherChannelConventions;
use Illuminate\Contracts\Auth\Authenticatable;
use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\AccessDeniedHttpException;

class Broadcaster extends BaseBroadcaster
{
    use UsePusherChannelConventions;

    protected string $appId;

    /**
     * @param array<mixed> $config
     */
    public function __construct(array $config = [])
    {
        $appId = $config['app_id'] ?? 'pogo-app';
        $this->appId = is_scalar($appId) ? (string) $appId : 'pogo-app';
    }

    /**
     * @param Request $request
     * @return mixed
     */
    public function auth($request)
    {
        $channelNameInput = $request->input('channel_name');
        $stringChannelName = is_string($channelNameInput) ? $channelNameInput : '';

        $channelName = $this->normalizeChannelName($stringChannelName);

        try {
            $result = $this->verifyUserCanAccessChannel($request, $channelName);
        } catch (AccessDeniedHttpException $e) {
            throw $e;
        }

        $response = [
            'auth' => $this->appId . ':dummy_signature_for_client',
        ];

        if (is_string($channelNameInput) && str_starts_with($channelNameInput, 'presence-')) {
            $user = $request->user();
            $userId = '';

            if ($user instanceof Authenticatable) {
                $id = $user->getAuthIdentifier();
                $userId = is_scalar($id) ? (string) $id : '';
            }

            /** @var array<string, mixed> $resultArray */
            $resultArray = (array) $result;

            $idFromRes = $resultArray['id'] ?? null;

            $channelData = [
                'user_id' => is_scalar($idFromRes) ? (string) $idFromRes : $userId,
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

    /**
     * @param array<mixed> $channels
     * @param mixed $event
     * @param array<mixed> $payload
     * @return void
     */
    public function broadcast(array $channels, $event, array $payload = [])
    {
        if (!function_exists('pogo_websocket_publish')) {
            return;
        }

        $payloadJson = json_encode($payload);

        if ($payloadJson === false) {
            return;
        }

        foreach ($channels as $channel) {
            $channelStr = is_scalar($channel) ? (string) $channel : '';
            $eventStr = is_scalar($event) ? (string) $event : '';

            if ($channelStr !== '' && $eventStr !== '') {
                pogo_websocket_publish($this->appId, $channelStr, $eventStr, $payloadJson);
            }
        }
    }
}