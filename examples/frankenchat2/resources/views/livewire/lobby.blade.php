<div class="p-6 bg-white border-b border-gray-200" x-data="{ 
        messages: [],
init() {
    Echo.channel('public-lobby')
        // Note le point au début : .lobby-message
        // Cela correspond exactement au return de broadcastAs()
        .listen('.lobby-message', (e) => {
            console.log('Message reçu !', e); // Debug log
            this.messages.push(e);
            this.$nextTick(() => {
                const container = document.getElementById('chat-container');
                if(container) container.scrollTop = container.scrollHeight;
            });
        });
}
    }">
    <div class="mb-4">
        <h2 class="text-xl font-bold text-gray-800">Public Lobby (Fan-out Test)</h2>
        <p class="text-sm text-gray-500">Channel: <code>public-lobby</code></p>
    </div>

    <div id="chat-container" class="h-64 overflow-y-auto bg-gray-50 p-4 border rounded mb-4 flex flex-col gap-2">
        <template x-for="msg in messages" :key="msg.time + msg.username">
            <div class="text-sm">
                <span class="text-gray-400" x-text="'[' + msg.time + ']'"></span>
                <span class="font-bold text-blue-600" x-text="msg.username + ':'"></span>
                <span class="text-gray-800" x-text="msg.message"></span>
            </div>
        </template>

        <div x-show="messages.length === 0" class="text-center text-gray-400 mt-10">
            Waiting for messages...
        </div>
    </div>

    <form wire:submit.prevent="sendMessage" class="flex gap-2">
        <input type="text" wire:model="message" class="flex-1 rounded border-gray-300 shadow-sm"
            placeholder="Type a message to broadcast..." autofocus>
        <button type="submit" class="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700 disabled:opacity-50"
            wire:loading.attr="disabled">
            Send
        </button>
    </form>
</div>