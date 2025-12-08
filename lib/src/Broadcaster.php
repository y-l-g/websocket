<?php

namespace Pogo\WebSocket;

use Illuminate\Broadcasting\Broadcasters\Broadcaster as BaseBroadcaster;
use Illuminate\Broadcasting\Broadcasters\UsePusherChannelConventions;
use Illuminate\Contracts\Auth\Authenticatable;
use Illuminate\Contracts\Support\Arrayable;
use Symfony\Component\HttpKernel\Exception\AccessDeniedHttpException;

class Broadcaster extends BaseBroadcaster
{
    use UsePusherChannelConventions;

    protected string $appId;
    protected string $secret;

    /**
     * @param array<string, mixed> $config
     */
    public function __construct(array $config = [])
    {
        // specific checks are replaced by safe casting which satisfies PHPStan
        $this->appId = isset($config['app_id']) && is_string($config['app_id'])
            ? $config['app_id']
            : 'pogo-app';

        $this->secret = isset($config['secret']) && is_string($config['secret'])
            ? $config['secret']
            : 'super-secret-key';
    }

    /**
     * Authenticate the incoming request for a given channel.
     *
     * @param  \Illuminate\Http\Request  $request
     * @return mixed
     */
    public function auth($request)
    {
        $channelNameInput = $request->input('channel_name');

        if (empty($channelNameInput)) {
            // @phpstan-ignore-next-line
            $channelNameInput = $request->channel_name;
        }

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

        if (str_starts_with($stringChannelName, 'presence-')) {
            $user = $request->user();
            $userId = '';

            if ($user instanceof Authenticatable) {
                // PHPStan requires explicit handling for mixed returns
                $id = $user->getAuthIdentifier();
                $userId = is_scalar($id) ? (string) $id : '';
            }

            /** @var array<string, mixed> $resultArray */
            $resultArray = is_array($result) ? $result : [];
            $idFromRes = $resultArray['id'] ?? null;

            $channelData = [
                'user_id' => is_scalar($idFromRes) ? (string) $idFromRes : $userId,
                'user_info' => $result,
            ];

            $response['channel_data'] = json_encode($channelData);
        }

        return $response;
    }

    /**
     * Authenticate the current user (Pusher User Auth / Watchlist).
     *
     * @param  \Illuminate\Http\Request  $request
     * @return mixed
     */
    public function authenticateUser($request)
    {
        $socketId = $request->input('socket_id');
        $stringSocketId = is_string($socketId) ? $socketId : '';

        $user = $request->user();

        if (!$user instanceof Authenticatable) {
            throw new AccessDeniedHttpException('User not authenticated');
        }

        $userInfo = [];
        if ($user instanceof Arrayable) {
            $userInfo = $user->toArray();
        } elseif (method_exists($user, 'toArray')) {
            $userInfo = $user->toArray();
        }

        $id = $user->getAuthIdentifier();

        $userData = [
            'id' => is_scalar($id) ? (string) $id : '',
            'user_info' => $userInfo,
        ];

        $userDataJson = json_encode($userData);
        if ($userDataJson === false) {
            $userDataJson = '{}';
        }

        $stringToSign = $stringSocketId . '::user::' . $userDataJson;
        $signature = hash_hmac('sha256', $stringToSign, $this->secret);

        return [
            'auth' => $this->appId . ':' . $signature,
            'user_data' => $userDataJson,
        ];
    }

    public function validAuthenticationResponse($request, $result)
    {
        return $result;
    }

    /**
     * Broadcast the given event.
     *
     * @param  array<string>  $channels
     * @param  string  $event
     * @param  array<mixed>  $payload
     * @return void
     */
    public function broadcast(array $channels, $event, array $payload = [])
    {
        if (empty($channels)) {
            return;
        }

        $payloadJson = json_encode($payload);
        if ($payloadJson === false) {
            return;
        }

        $eventStr = (string) $event;

        if (function_exists('pogo_websocket_broadcast_multi')) {
            $validChannels = [];
            foreach ($channels as $channel) {
                $s = (string) $channel;
                if ($s !== '') {
                    $validChannels[] = $s;
                }
            }

            if (!empty($validChannels)) {
                $channelsJson = json_encode($validChannels);
                if ($channelsJson !== false) {
                    $result = pogo_websocket_broadcast_multi($this->appId, $channelsJson, $eventStr, $payloadJson);
                    if ($result) {
                        return;
                    }
                }
            }
        }

        if (!function_exists('pogo_websocket_publish')) {
            return;
        }

        foreach ($channels as $channel) {
            $channelStr = (string) $channel;
            if ($channelStr !== '' && $eventStr !== '') {
                pogo_websocket_publish($this->appId, $channelStr, $eventStr, $payloadJson);
            }
        }
    }
}
