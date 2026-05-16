<?php

namespace Pogo\WebSocket;

use Illuminate\Broadcasting\Broadcasters\Broadcaster as BaseBroadcaster;
use Illuminate\Broadcasting\Broadcasters\UsePusherChannelConventions;
use Illuminate\Broadcasting\BroadcastException;
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
            'auth' => $this->signChannelAuth($stringChannelName),
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

        $payloadJson = $this->encodeBroadcastPayload($payload);
        if ($payloadJson === false) {
            $this->throwBroadcastError('payload_encode_failed', $channels, (string) $event);
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
                    if ($result === 0) {
                        return;
                    }
                    $this->throwBroadcastError('broadcast_multi_failed', $validChannels, $eventStr, 'pogo_websocket_broadcast_multi', $result);
                } else {
                    $this->throwBroadcastError('channels_json_encode_failed', $channels, $eventStr);
                }
            }
        }

        if (!function_exists('pogo_websocket_publish')) {
            $this->throwBroadcastError('pogo_extension_not_loaded', $channels, $eventStr);
        }

        foreach ($channels as $channel) {
            $channelStr = (string) $channel;
            if ($channelStr !== '' && $eventStr !== '') {
                $result = pogo_websocket_publish($this->appId, $channelStr, $eventStr, $payloadJson);
                if ($result !== 0) {
                    $this->throwBroadcastError('publish_failed', [$channelStr], $eventStr, 'pogo_websocket_publish', $result);
                }
            }
        }
    }

    /**
     * @param  array<string>  $channels
     */
    protected function throwBroadcastError(string $reason, array $channels, string $event, ?string $function = null, ?int $status = null): never
    {
        $nativeStatus = $status === null ? '' : sprintf(' status=%d(%s)', $status, $this->nativeStatusReason($status));
        $nativeFunction = $function === null ? '' : sprintf(' function=%s', $function);

        throw new BroadcastException(sprintf(
            '[Pogo WebSocket] Broadcast failed: reason=%s app_id=%s event=%s channels=%s%s%s',
            $reason,
            $this->appId,
            $event,
            implode(',', $channels),
            $nativeFunction,
            $nativeStatus
        ));
    }

    protected function nativeStatusReason(int $status): string
    {
        return match ($status) {
            0 => 'success',
            1 => 'hub_missing',
            2 => 'channel_too_long',
            3 => 'event_too_long',
            4 => 'payload_too_large',
            5 => 'invalid_payload_json',
            6 => 'broker_publish_failed',
            7 => 'invalid_channels_json',
            default => 'unknown',
        };
    }

    /**
     * @param  array<mixed>  $payload
     * @return string|false
     */
    protected function encodeBroadcastPayload(array $payload)
    {
        return json_encode($payload);
    }

    protected function signChannelAuth(string $channelName): string
    {
        $signature = hash_hmac('sha256', $channelName, $this->secret);
        return $this->appId . ':' . $signature;
    }
}
