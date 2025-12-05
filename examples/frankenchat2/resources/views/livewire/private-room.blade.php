<div class="p-6 bg-yellow-50 border-b border-yellow-200 mt-6" x-data="{ 
        messages: [],
        init() {
            // Note the 'private-' prefix via echo.private()
            Echo.private('room.{{ $roomId }}')
                .listen('.private-message', (e) => {
                    console.log('🔒 Private Msg:', e);
                    this.messages.push(e);
                })
                .error((error) => {
                    console.error('Auth Error:', error);
                    alert('Impossible to join private room');
                });
        }
    }">
    <div class="mb-4">
        <h2 class="text-xl font-bold text-yellow-800">🔒 Private Room #{{ $roomId }}</h2>
        <p class="text-sm text-yellow-600">
            Channel: <code>private-room.{{ $roomId }}</code><br>
            <span class="text-xs">Test authentication via PHP Worker.</span>
        </p>
    </div>

    <div class="h-40 overflow-y-auto bg-white p-4 border border-yellow-300 rounded mb-4 flex flex-col gap-2">
        <template x-for="msg in messages" :key="Math.random()">
            <div class="text-sm">
                <span class="font-bold text-yellow-700" x-text="msg.username + ':'"></span>
                <span class="text-gray-800" x-text="msg.message"></span>
            </div>
        </template>
        <div x-show="messages.length === 0" class="text-center text-gray-400 mt-4">
            Room secured. Speak...
        </div>
    </div>

    <form wire:submit.prevent="sendMessage" class="flex gap-2">
        <input type="text" wire:model="message" class="flex-1 rounded border-yellow-300 shadow-sm"
            placeholder="Secret message...">
        <button type="submit" class="px-4 py-2 bg-yellow-600 text-white rounded hover:bg-yellow-700">
            Send Secret
        </button>
    </form>
</div>