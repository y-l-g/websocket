<?php

namespace Pogo\WebSocket\Tests\Unit;

use Mockery;
use PHPUnit\Framework\TestCase;
use Pogo\WebSocket\Broadcaster;
use Illuminate\Http\Request;
use Illuminate\Broadcasting\BroadcastException;
use Illuminate\Contracts\Auth\Authenticatable;
use Illuminate\Contracts\Support\Arrayable;
use InvalidArgumentException;

class BroadcasterTest extends TestCase
{
    protected function tearDown(): void
    {
        Mockery::close();
    }

    public function testAuthGeneratesCorrectSignature()
    {
        $request = Mockery::mock(Request::class);
        // Handle Laravel's magic getter $request->channel_name calling all()
        $request->shouldReceive('all')->andReturn(['channel_name' => 'private-test']);
        $request->shouldReceive('input')->with('channel_name')->andReturn('private-test');
        $request->shouldReceive('input')->with('socket_id')->andReturn('1.1');
        $request->shouldReceive('user')->andReturn(null);
        // Handle direct property access on mock
        $request->shouldReceive('__get')->with('channel_name')->andReturn('private-test');

        $broadcaster = Mockery::mock(Broadcaster::class, [['app_id' => 'test-app', 'secret' => 'super-secret']])->makePartial();
        $broadcaster->shouldAllowMockingProtectedMethods();
        $broadcaster->shouldReceive('verifyUserCanAccessChannel')->andReturn(true);
        $broadcaster->shouldReceive('normalizeChannelName')->andReturn('private-test');

        $response = $broadcaster->auth($request);

        $this->assertIsArray($response);
        $expectedSig = hash_hmac('sha256', '1.1:private-test', 'super-secret');
        $this->assertEquals('test-app:' . $expectedSig, $response['auth']);
    }

    public function testConstructorRequiresExplicitCredentials()
    {
        $this->expectException(InvalidArgumentException::class);
        new Broadcaster(['app_id' => 'test-app']);
    }

    public function testPresenceChannelReturnsUserData()
    {
        $broadcaster = Mockery::mock(Broadcaster::class, [['app_id' => 'test-app', 'secret' => 'super-secret']])->makePartial();
        $broadcaster->shouldAllowMockingProtectedMethods();

        $user = Mockery::mock(Authenticatable::class);
        $user->shouldReceive('getAuthIdentifier')->andReturn(42);

        $request = Mockery::mock(Request::class);
        $request->shouldReceive('all')->andReturn(['channel_name' => 'presence-chat']);
        $request->shouldReceive('input')->with('channel_name')->andReturn('presence-chat');
        $request->shouldReceive('input')->with('socket_id')->andReturn('1.2');
        $request->shouldReceive('__get')->with('channel_name')->andReturn('presence-chat');
        $request->shouldReceive('user')->andReturn($user);

        $userInfo = ['name' => 'John Doe'];
        $broadcaster->shouldReceive('verifyUserCanAccessChannel')->andReturn($userInfo);
        $broadcaster->shouldReceive('normalizeChannelName')->andReturn('presence-chat');

        $response = $broadcaster->auth($request);

        $this->assertArrayHasKey('channel_data', $response);
        $channelData = json_decode($response['channel_data'], true);

        $this->assertEquals('42', $channelData['user_id']);
        $this->assertEquals($userInfo, $channelData['user_info']);

        $expectedSig = hash_hmac('sha256', '1.2:presence-chat:' . $response['channel_data'], 'super-secret');
        $this->assertEquals('test-app:' . $expectedSig, $response['auth']);
    }

    public function testAuthenticateUser()
    {
        $broadcaster = new Broadcaster([
            'app_id' => 'test-app',
            'secret' => 'my-secret',
        ]);

        $user = Mockery::mock(Authenticatable::class, Arrayable::class);
        $user->shouldReceive('getAuthIdentifier')->andReturn(123);
        $user->shouldReceive('toArray')->andReturn(['name' => 'Alice']);

        $request = Mockery::mock(Request::class);
        $request->shouldReceive('input')->with('socket_id')->andReturn('1.1');
        $request->shouldReceive('user')->andReturn($user);

        $response = $broadcaster->authenticateUser($request);

        $this->assertArrayHasKey('auth', $response);
        $this->assertArrayHasKey('user_data', $response);

        // Verify Signature
        $userDataJson = json_encode(['id' => '123', 'user_info' => ['name' => 'Alice']]);
        $expectedString = "1.1::user::{$userDataJson}";
        $expectedSig = hash_hmac('sha256', $expectedString, 'my-secret');

        $this->assertEquals("test-app:{$expectedSig}", $response['auth']);
        $this->assertEquals($userDataJson, $response['user_data']);
    }

    public function testBroadcastThrowsWhenExtensionMissing()
    {
        $broadcaster = new class (['app_id' => 'test-app', 'secret' => 'super-secret']) extends Broadcaster {
            protected function hasBroadcastMulti(): bool
            {
                return false;
            }

            protected function hasPublish(): bool
            {
                return false;
            }
        };

        $this->expectException(BroadcastException::class);
        $this->expectExceptionMessage('pogo_extension_not_loaded');
        $broadcaster->broadcast(['test-channel'], 'test-event', ['foo' => 'bar']);
    }

    public function testBroadcastReportsNativeMultiFailure()
    {
        $broadcaster = new class (['app_id' => 'test-app', 'secret' => 'super-secret']) extends Broadcaster {
            protected function hasBroadcastMulti(): bool
            {
                return true;
            }

            protected function broadcastMulti(string $channelsJson, string $event, string $payloadJson): int
            {
                return 1;
            }
        };

        $this->expectException(BroadcastException::class);
        $this->expectExceptionMessage('broadcast_multi_failed');
        $this->expectExceptionMessage('status=1(hub_missing)');
        $broadcaster->broadcast(['test-channel'], 'test-event', ['foo' => 'bar']);
    }

    public function testNonBenchmarkAndFailedPayloadEncodingKeepExistingBehavior()
    {
        $broadcaster = new class (['app_id' => 'test-app', 'secret' => 'super-secret']) extends Broadcaster {
            /**
             * @param array<mixed> $payload
             * @return string|false
             */
            public function encodeForTest(array $payload)
            {
                return $this->encodeBroadcastPayload($payload);
            }
        };

        $payloadJson = $broadcaster->encodeForTest(['foo' => 'bar']);
        $this->assertSame(['foo' => 'bar'], json_decode((string) $payloadJson, true));

        $this->assertFalse($broadcaster->encodeForTest(['invalid' => INF]));
    }

    public function testFailedPayloadEncodingThrowsBroadcastException()
    {
        $broadcaster = new class (['app_id' => 'test-app', 'secret' => 'super-secret']) extends Broadcaster {
            /**
             * @param array<mixed> $payload
             * @return string|false
             */
            protected function encodeBroadcastPayload(array $payload)
            {
                return false;
            }
        };

        $this->expectException(BroadcastException::class);
        $this->expectExceptionMessage('payload_encode_failed');
        $broadcaster->broadcast(['test-channel'], 'test-event', ['foo' => 'bar']);
    }

    public function testNativeStatusReasonsIncludeOverloadFailures()
    {
        $broadcaster = new class (['app_id' => 'test-app', 'secret' => 'super-secret']) extends Broadcaster {
            public function reasonFor(int $status): string
            {
                return $this->nativeStatusReason($status);
            }
        };

        $this->assertSame('broker_queue_full', $broadcaster->reasonFor(8));
        $this->assertSame('shard_queue_full', $broadcaster->reasonFor(9));
    }
}
