<div class="p-6 bg-red-50 border-b border-red-200 mt-6" x-data="{ 
        users: [],
        typingUser: null,
        typingTimer: null,
        
        init() {
            console.log('🚀 Initialisation War Room...');
            
            // On s'assure de partir propre
            if (window.Echo) {
                window.Echo.leave('war-room');
            }

            window.Echo.join('war-room')
                .here((users) => {
                    console.log('👥 HERE Reçu (Brut):', users);
                    
                    // Normalisation : Echo renvoie parfois un Objet {id: user}, parfois un Array [user]
                    // On veut toujours un Array pour Alpine
                    this.users = Array.isArray(users) ? users : Object.values(users);
                    
                    console.log('✅ Liste Initialisée:', this.users.length, 'agents');
                })
                .joining((user) => {
                    console.log('➕ JOINING Reçu:', user);
                    
                    // Vérifier qu'il n'est pas déjà là (doublon)
                    if (!this.users.find(u => u.id === user.id)) {
                        this.users.push(user);
                        console.log(' -> Ajouté. Nouveau total:', this.users.length);
                    } else {
                        console.log(' -> Ignoré (Déjà présent)');
                    }
                })
                .leaving((user) => {
                    console.log('➖ LEAVING Reçu:', user);
                    this.users = this.users.filter(u => u.id !== user.id);
                    console.log(' -> Retiré. Nouveau total:', this.users.length);
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
                    name: '{{ auth()->user()->name }}'
                });
        }
    }">

    <div class="mb-4 flex justify-between items-center">
        <div>
            <h2 class="text-xl font-bold text-red-800">☢️ War Room</h2>
            <p class="text-sm text-red-600">ID Moi: <strong>{{ auth()->id() }}</strong></p>
        </div>
        <div class="text-right">
            <!-- Utilisation de x-text pour être sûr du réactif -->
            <span class="text-2xl font-bold" x-text="users.length">0</span>
            <span class="text-xs uppercase text-red-600 font-bold">Agents</span>
        </div>
    </div>

    <!-- Debug Visuel de la liste -->
    <div class="mb-6 p-2 bg-gray-100 rounded text-xs font-mono" style="max-height: 100px; overflow:auto;">
        <strong>DEBUG DATA:</strong> <span x-text="JSON.stringify(users)"></span>
    </div>

    <div class="mb-6">
        <h3 class="text-xs font-bold text-gray-500 uppercase mb-2">Active Agents</h3>
        <div class="flex flex-wrap gap-2">
            <template x-for="user in users" :key="user.id">
                <div class="flex items-center gap-2 px-3 py-1 bg-white rounded-full border border-red-200 shadow-sm">
                    <div class="w-2 h-2 rounded-full bg-green-500 animate-pulse"></div>
                    <span class="text-sm font-medium text-gray-700" x-text="user.name + ' (ID:' + user.id + ')'"></span>
                </div>
            </template>
        </div>
    </div>

    <div class="h-10 bg-black rounded flex items-center justify-center">
        <span x-show="typingUser" class="text-green-400 font-mono text-sm" x-text="typingUser + ' tape...'"></span>
    </div>

    <input type="text" class="w-full mt-4 border p-2" placeholder="Tapez ici..." @keydown="sendTyping()">
</div>