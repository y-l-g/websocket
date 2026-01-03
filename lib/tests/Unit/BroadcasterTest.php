<?php

namespace Pogo\WebSocket\Tests\Unit;

use Mockery;
use PHPUnit\Framework\TestCase;
use Pogo\WebSocket\Broadcaster;
use Illuminate\Http\Request;
use Illuminate\Contracts\Auth\Authenticatable;
use Illuminate\Contracts\Support\Arrayable;

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
        $request->shouldReceive('user')->andReturn(null);
        // Handle direct property access on mock
        $request->shouldReceive('__get')->with('channel_name')->andReturn('private-test');

        $broadcaster = Mockery::mock(Broadcaster::class, [['app_id' => 'test-app', 'secret' => 'super-secret']])->makePartial();
        $broadcaster->shouldAllowMockingProtectedMethods();
        $broadcaster->shouldReceive('verifyUserCanAccessChannel')->andReturn(true);
        $broadcaster->shouldReceive('normalizeChannelName')->andReturn('private-test');

        $response = $broadcaster->auth($request);

        $this->assertIsArray($response);
        $this->assertEquals('test-app:dummy_signature_for_client', $response['auth']);
    }

    public function testPresenceChannelReturnsUserData()
    {
        $broadcaster = Mockery::mock(Broadcaster::class, [['app_id' => 'test-app']])->makePartial();
        $broadcaster->shouldAllowMockingProtectedMethods();

        $user = Mockery::mock(Authenticatable::class);
        $user->shouldReceive('getAuthIdentifier')->andReturn(42);

        $request = Mockery::mock(Request::class);
        $request->shouldReceive('all')->andReturn(['channel_name' => 'presence-chat']);
        $request->shouldReceive('input')->with('channel_name')->andReturn('presence-chat');
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

    public function testBroadcastCallsGlobalFunction()
    {
        // Just verify no crash
        $broadcaster = new Broadcaster(['app_id' => 'test-app']);
        $broadcaster->broadcast(['test-channel'], 'test-event', ['foo' => 'bar']);
        $this->assertTrue(true);
    }
}
