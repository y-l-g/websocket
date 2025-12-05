<?php

use Livewire\Volt\Component;
use Illuminate\Support\Facades\Auth;
use Illuminate\Contracts\Broadcasting\ShouldBroadcastNow;
use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\PrivateChannel;

new class extends Component {
    // --- STATE ---
    public string $lobbyInput = '';
    public string $privateInput = '';

    // --- ACTIONS ---

    public function dispatchLobby()
    {
        if (empty(trim($this->lobbyInput))) {
            return;
        }

        $user = Auth::user();
        $msg = $this->lobbyInput;

        broadcast(new class ($user->name, $msg) implements ShouldBroadcastNow {
            public string $username;
            public string $message;
            public string $time;

            public function __construct($username, $message)
            {
                $this->username = $username;
                $this->message = $message;
                $this->time = now()->format('H:i:s');
            }

            public function broadcastOn(): array
            {
                return [new Channel('public-lobby')];
            }
            public function broadcastAs(): string
            {
                return 'lobby-message';
            }
        });

        $this->lobbyInput = '';
    }

    public function dispatchPrivate()
    {
        if (empty(trim($this->privateInput))) {
            return;
        }

        $user = Auth::user();
        $msg = $this->privateInput;

        broadcast(new class ($user->name, $msg) implements ShouldBroadcastNow {
            public string $username;
            public string $message;
            public int $roomId = 1;

            public function __construct($username, $message)
            {
                $this->username = $username;
                $this->message = $message;
            }

            public function broadcastOn(): array
            {
                return [new PrivateChannel('room.' . $this->roomId)];
            }
            public function broadcastAs(): string
            {
                return 'private-message';
            }
        });

        $this->privateInput = '';
    }
}; ?>

<div class="min-h-screen bg-gray-900 text-gray-100 p-6 font-mono">

    <!-- HEADER -->
    <div class="mb-8 flex justify-between items-end border-b border-gray-700 pb-4">
        <div>
            <h1 class="text-3xl font-bold text-green-500">FrankenChat Console</h1>
            <p class="text-gray-400">Integrated Stress Test Unit</p>
        </div>
        <div class="text-right text-xs">
            <div class="mb-1">Agent: <span class="text-white font-bold">{{ Auth::user()->name }}</span> (ID:
                {{ Auth::id() }})
            </div>
            <div class="flex items-center justify-end gap-2">
                <span class="w-3 h-3 bg-green-500 rounded-full animate-pulse"></span>
                <span>System Online</span>
            </div>
        </div>
    </div>

    <!-- GRID LAYOUT -->
    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">

        <!-- PANEL 1: PUBLIC LOBBY -->
        <div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden flex flex-col h-[650px]" x-data="{ 
                messages: [],
                init() {
                    Echo.channel('public-lobby')
                        .listen('.lobby-message', (e) => {
                            this.messages.push(e);
                            this.scrollToBottom();
                        });
                },
                scrollToBottom() {
                    this.$nextTick(() => { this.$refs.scroll.scrollTop = this.$refs.scroll.scrollHeight; });
                }
            }">
            <div class="bg-blue-900/30 p-3 border-b border-blue-800">
                <h2 class="font-bold text-blue-400">📡 Public Lobby</h2>
            </div>
            <div x-ref="scroll" class="flex-1 overflow-y-auto p-4 space-y-2">
                <template x-for="msg in messages">
                    <div class="text-sm animate-in fade-in slide-in-from-bottom-2 duration-300">
                        <span class="text-gray-500" x-text="'[' + msg.time + ']'"></span>
                        <span class="font-bold text-blue-400" x-text="msg.username"></span>:
                        <span class="text-gray-300" x-text="msg.message"></span>
                    </div>
                </template>
                <div x-show="messages.length === 0" class="text-center text-gray-600 mt-10 italic">Channel Open. Waiting
                    for broadcast...</div>
            </div>
            <div class="p-3 bg-gray-900 border-t border-gray-700">
                <form wire:submit="dispatchLobby" class="flex gap-2">
                    <input type="text" wire:model="lobbyInput"
                        class="flex-1 bg-gray-800 border-gray-600 rounded px-3 py-2 text-sm focus:border-blue-500 outline-none"
                        placeholder="Broadcast...">
                    <button type="submit"
                        class="bg-blue-600 hover:bg-blue-500 text-white px-4 rounded text-sm">Send</button>
                </form>
            </div>
        </div>

        <!-- PANEL 2: PRIVATE ROOM -->
        <div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden flex flex-col h-[650px]" x-data="{ 
                messages: [],
                connected: false,
                init() {
                    Echo.private('room.1')
                        .listen('.private-message', (e) => {
                            this.messages.push(e);
                            this.scrollToBottom();
                        })
                        .on('pusher:subscription_succeeded', () => this.connected = true);
                },
                scrollToBottom() {
                    this.$nextTick(() => { this.$refs.scroll.scrollTop = this.$refs.scroll.scrollHeight; });
                }
            }">
            <div class="bg-yellow-900/30 p-3 border-b border-yellow-800 flex justify-between">
                <h2 class="font-bold text-yellow-500">🔒 Private Room #1</h2>
                <span x-show="connected"
                    class="text-xs text-green-400 border border-green-700 px-1 rounded bg-green-900/20">Auth
                    Verified</span>
            </div>
            <div x-ref="scroll" class="flex-1 overflow-y-auto p-4 space-y-2">
                <template x-for="msg in messages">
                    <div class="text-sm animate-in fade-in slide-in-from-bottom-2 duration-300">
                        <span class="font-bold text-yellow-500" x-text="msg.username"></span> ➜
                        <span class="text-white" x-text="msg.message"></span>
                    </div>
                </template>
                <div x-show="messages.length === 0" class="text-center text-gray-600 mt-10 italic">Secure Channel.</div>
            </div>
            <div class="p-3 bg-gray-900 border-t border-gray-700">
                <form wire:submit="dispatchPrivate" class="flex gap-2">
                    <input type="text" wire:model="privateInput"
                        class="flex-1 bg-gray-800 border-gray-600 rounded px-3 py-2 text-sm focus:border-yellow-500 outline-none"
                        placeholder="Encrypted msg...">
                    <button type="submit"
                        class="bg-yellow-600 hover:bg-yellow-500 text-white px-4 rounded text-sm">Send</button>
                </form>
            </div>
        </div>

        <!-- PANEL 3: WAR ROOM (PRESENCE & WHISPERS) -->
        <div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden flex flex-col h-[650px]" x-data="{ 
                users: [],
                typingUser: null,
                typingTimer: null,
                myId: {{ Auth::id() }},
                
                init() {
                    console.log('🚀 Initialisation War Room...');
                    
                    // 1. Nettoyage préventif (Fix User)
                    if (window.Echo) {
                        window.Echo.leave('war-room');
                    }

                    window.Echo.join('war-room')
                        .here((users) => {
                            console.log('👥 HERE Reçu:', users);
                            // 2. Normalisation Objet -> Array (Fix User)
                            this.users = Array.isArray(users) ? users : Object.values(users);
                        })
                        .joining((user) => {
                            console.log('➕ JOINING:', user);
                            // 3. Anti-doublon (Fix User)
                            if (!this.users.find(u => u.id === user.id)) {
                                this.users.push(user);
                            }
                        })
                        .leaving((user) => {
                            console.log('➖ LEAVING:', user);
                            this.users = this.users.filter(u => u.id !== user.id);
                        })
                        .listenForWhisper('typing', (e) => {
                            console.log('⚡ Whisper:', e);
                            this.typingUser = e.name;
                            if (this.typingTimer) clearTimeout(this.typingTimer);
                            this.typingTimer = setTimeout(() => this.typingUser = null, 2000);
                        });
                },
                
                sendTyping() {
                    window.Echo.join('war-room')
                        .whisper('typing', {
                            name: '{{ Auth::user()->name }}'
                        });
                }
            }">
            <div class="bg-red-900/30 p-3 border-b border-red-800 flex justify-between items-center">
                <div>
                    <h2 class="font-bold text-red-500">☢️ War Room</h2>
                    <div class="text-xs text-red-300">State & Whispers</div>
                </div>
                <div class="text-right">
                    <div class="text-2xl font-bold text-red-500 leading-none" x-text="users.length">0</div>
                    <div class="text-[10px] uppercase text-red-400">Agents</div>
                </div>
            </div>

            <div class="flex-1 flex flex-col p-4">

                <!-- WHISPER MONITOR -->
                <div
                    class="h-24 mb-4 bg-black rounded border border-red-900/50 flex items-center justify-center relative overflow-hidden">
                    <!-- Scanline effect -->
                    <div
                        class="absolute inset-0 bg-gradient-to-b from-transparent via-red-900/10 to-transparent pointer-events-none animate-scan">
                    </div>

                    <template x-if="typingUser">
                        <div class="text-green-500 font-mono text-lg animate-pulse flex items-center gap-2">
                            <span>></span> <span x-text="typingUser"></span> is typing... <span
                                class="animate-ping">_</span>
                        </div>
                    </template>
                    <template x-if="!typingUser">
                        <div class="text-red-900/40 font-mono text-sm">Waiting for client signal...</div>
                    </template>
                </div>

                <!-- USER LIST -->
                <div class="flex justify-between items-end mb-2">
                    <h3 class="text-xs uppercase text-gray-500">Active Agents</h3>
                    <div class="text-[10px] text-gray-600 font-mono" x-show="users.length > 0">SYNCED</div>
                </div>

                <div class="flex-1 overflow-y-auto grid grid-cols-2 gap-2 content-start pr-1">
                    <template x-for="user in users" :key="user.id">
                        <div class="flex items-center gap-2 p-2 rounded border transition-all duration-300"
                            :class="user.id === myId ? 'bg-green-900/20 border-green-700' : 'bg-gray-700/50 border-gray-600'">
                            <div class="relative">
                                <div class="w-2 h-2 rounded-full bg-green-500"></div>
                                <div class="absolute inset-0 w-2 h-2 rounded-full bg-green-500 animate-ping opacity-75">
                                </div>
                            </div>
                            <div class="flex-1 min-w-0">
                                <div class="text-xs font-bold text-gray-200 truncate" x-text="user.name"></div>
                                <div class="text-[10px] text-gray-400 font-mono" x-text="'ID: ' + user.id"></div>
                            </div>
                        </div>
                    </template>
                </div>

                <!-- DEBUG DATA (Optional, hidden by default but useful) -->
                <!-- <div class="mt-2 text-[10px] text-gray-700 break-all font-mono" x-text="JSON.stringify(users)"></div> -->

                <!-- INPUT -->
                <div class="mt-4 pt-4 border-t border-gray-700">
                    <input type="text"
                        class="w-full bg-gray-900 border border-gray-600 text-red-400 text-sm rounded px-3 py-2 focus:border-red-500 focus:ring-1 focus:ring-red-500 outline-none placeholder-gray-600 transition-colors"
                        placeholder="Type to trigger Whisper..." @keydown="sendTyping()">
                    <div class="text-[10px] text-gray-500 mt-1 text-right">
                        Direct P2P Signal (No PHP)
                    </div>
                </div>
            </div>
        </div>

    </div>
</div>

{{-- <style>
    @keyframes scan {
        0% {
            transform: translateY(-100%);
        }

        100% {
            transform: translateY(100%);
        }
    }

    .animate-scan {
        animation: scan 3s linear infinite;
    }
</style> --}}