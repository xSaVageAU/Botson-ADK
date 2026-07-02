let currentSessionId = '';

// Tab Switching logic
const navBtns = document.querySelectorAll('.nav-btn');
const tabPanes = document.querySelectorAll('.tab-pane');

navBtns.forEach(btn => {
    btn.addEventListener('click', () => {
        const targetId = btn.getAttribute('data-target');
        
        navBtns.forEach(b => b.classList.remove('active'));
        tabPanes.forEach(p => p.classList.remove('active'));
        
        btn.classList.add('active');
        const targetPane = document.getElementById(targetId);
        if (targetPane) {
            targetPane.classList.add('active');
        }

        // Fetch view-specific data
        if (targetId === 'pane-config') {
            loadConfig();
        } else if (targetId === 'pane-sandbox') {
            loadSandbox();
        } else if (targetId === 'pane-pairings') {
            loadPairings();
        }
    });
});

// Chat client loops
const form = document.getElementById('input-form');
const input = document.getElementById('message-input');
const container = document.getElementById('chat-container');
const sendBtn = document.getElementById('send-btn');

form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || !currentSessionId) return;

    input.value = '';
    input.disabled = true;
    sendBtn.disabled = true;

    // Append User message
    appendMessage(text, 'user');

    // Show Typing Indicator
    const typingIndicator = showTypingIndicator();
    container.scrollTop = container.scrollHeight;

    try {
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message: text, session_id: currentSessionId })
        });
        const data = await response.json();
        
        typingIndicator.remove();

        if (data.error) {
            appendMessage('❌ Error: ' + data.error, 'agent');
        } else {
            appendMessage(data.response, 'agent');
        }
    } catch (err) {
        typingIndicator.remove();
        appendMessage('❌ Connection error to the local server.', 'agent');
    } finally {
        input.disabled = false;
        sendBtn.disabled = false;
        input.focus();
        container.scrollTop = container.scrollHeight;
    }
});

function appendMessage(text, sender) {
    const div = document.createElement('div');
    div.classList.add('message', sender);
    
    if (sender === 'agent') {
        let formatted = text
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/`([^`\n]+)`/g, '<code>$1</code>');
        
        const codeBlocks = formatted.split('```');
        let result = '';
        for (let i = 0; i < codeBlocks.length; i++) {
            if (i % 2 === 1) {
                const lines = codeBlocks[i].split('\n');
                const lang = lines[0];
                const code = lines.slice(1).join('\n').trim();
                result += '<pre><code class="language-' + lang + '">' + code + '</code></pre>';
            } else {
                result += codeBlocks[i].replace(/\n/g, '<br>');
            }
        }
        div.innerHTML = result;
    } else {
        div.textContent = text;
    }
    
    container.appendChild(div);
}

function showTypingIndicator() {
    const div = document.createElement('div');
    div.classList.add('typing-indicator');
    div.innerHTML = '<span></span><span></span><span></span>';
    container.appendChild(div);
    return div;
}

// Config Panel Helpers
async function loadConfig() {
    try {
        const response = await fetch('/api/config');
        const data = await response.json();
        
        document.getElementById('cfg-provider').value = data.provider || 'openrouter';
        document.getElementById('cfg-instruction').value = data.instruction || '';
        document.getElementById('cfg-discord-token').value = data.discord_token_masked || '';
        
        document.getElementById('cfg-sandboxing').checked = data.sandboxing || false;
        document.getElementById('cfg-services').checked = data.services || false;
        document.getElementById('cfg-coder').checked = data.coder || false;
        
        document.getElementById('cfg-or-model').value = data.openrouter_model || '';
        document.getElementById('cfg-or-key').value = data.openrouter_key_mask || '';
        
        document.getElementById('cfg-gemini-model').value = data.gemini_model || '';
        document.getElementById('cfg-gemini-key').value = data.gemini_key_mask || '';
    } catch (err) {
        console.error('Failed to load configurations', err);
    }
}

const configForm = document.getElementById('config-form');
configForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const payload = {
        provider: document.getElementById('cfg-provider').value,
        instruction: document.getElementById('cfg-instruction').value,
        discord_token: document.getElementById('cfg-discord-token').value,
        sandboxing: document.getElementById('cfg-sandboxing').checked,
        services: document.getElementById('cfg-services').checked,
        coder: document.getElementById('cfg-coder').checked,
        openrouter_model: document.getElementById('cfg-or-model').value,
        openrouter_key: document.getElementById('cfg-or-key').value,
        gemini_model: document.getElementById('cfg-gemini-model').value,
        gemini_key: document.getElementById('cfg-gemini-key').value
    };

    try {
        const response = await fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        const data = await response.json();
        alert(data.message || 'Configuration saved successfully!');
        loadConfig();
    } catch (err) {
        alert('Failed to save configuration details.');
    }
});

// Sandbox Panel Helpers
async function loadSandbox() {
    try {
        const response = await fetch('/api/sandbox');
        const data = await response.json();
        
        document.getElementById('sb-status-text').textContent = data.sandboxing_enabled ? 'Active (Isolated)' : 'Disabled';
        document.getElementById('sb-status-text').className = 'status-value ' + (data.sandboxing_enabled ? 'online' : 'offline');
        
        document.getElementById('sb-wsl-text').textContent = data.wsl_installed ? 'Installed (Provisioned)' : 'Not Installed';
        document.getElementById('sb-wsl-text').className = 'status-value ' + (data.wsl_installed ? 'online' : 'offline');
        
        document.getElementById('sb-cache-text').textContent = data.cache_dir || 'N/A';
    } catch (err) {
        console.error('Failed to load sandbox configurations', err);
    }
}

const btnWslSetup = document.getElementById('btn-wsl-setup');
btnWslSetup.addEventListener('click', async () => {
    btnWslSetup.disabled = true;
    btnWslSetup.textContent = 'Installing/Provisioning WSL...';
    try {
        const response = await fetch('/api/sandbox/setup', { method: 'POST' });
        const data = await response.json();
        alert(data.message || 'WSL setup triggered in background.');
    } catch (err) {
        alert('Failed to initiate WSL setup.');
    } finally {
        setTimeout(loadSandbox, 5000);
        btnWslSetup.disabled = false;
        btnWslSetup.textContent = 'Install & Provision WSL Sandbox';
    }
});

// Pending Pairings Panel Helpers
async function loadPairings() {
    const tbody = document.getElementById('pairings-tbody');
    try {
        const response = await fetch('/api/pairings');
        const data = await response.json();
        
        tbody.innerHTML = '';
        if (!data || data.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-state">No pending pairing approval requests found.</td></tr>';
            return;
        }

        data.forEach(p => {
            const tr = document.createElement('tr');
            
            const tdGateway = document.createElement('td');
            tdGateway.textContent = p.gateway;
            tr.appendChild(tdGateway);
            
            const tdUserID = document.createElement('td');
            tdUserID.textContent = p.user_id;
            tr.appendChild(tdUserID);
            
            const tdUsername = document.createElement('td');
            tdUsername.textContent = p.username;
            tr.appendChild(tdUsername);
            
            const tdCode = document.createElement('td');
            tdCode.textContent = p.code;
            tr.appendChild(tdCode);
            
            const tdActions = document.createElement('td');
            const btnApprove = document.createElement('button');
            btnApprove.textContent = 'Approve';
            btnApprove.classList.add('approve-btn');
            btnApprove.addEventListener('click', () => approvePairing(p.gateway, p.code));
            tdActions.appendChild(btnApprove);
            tr.appendChild(tdActions);
            
            tbody.appendChild(tr);
        });
    } catch (err) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Failed to query pending pairings.</td></tr>';
    }
}

async function approvePairing(gateway, code) {
    if (!confirm(`Are you sure you want to approve user authorization code "${code}" on gateway "${gateway}"?`)) {
        return;
    }
    
    try {
        const response = await fetch('/api/pairings/approve', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ gateway, code })
        });
        const data = await response.json();
        if (data.status === 'success') {
            alert(data.message);
            loadPairings();
        } else {
            alert('Error: ' + data.message);
        }
    } catch (err) {
        alert('Failed to send pairing approval request.');
    }
}

// Session Management Helpers
async function loadSessions() {
    const listContainer = document.getElementById('session-list');
    try {
        const response = await fetch('/api/sessions');
        const data = await response.json();
        
        listContainer.innerHTML = '';
        if (!data || data.length === 0) {
            // Start a new session automatically
            createSession();
            return;
        }

        // Render sessions
        data.forEach(s => {
            const btn = document.createElement('button');
            btn.classList.add('session-item');
            if (s.id === currentSessionId) {
                btn.classList.add('active');
            }
            btn.setAttribute('data-id', s.id);
            
            const label = document.createElement('span');
            label.classList.add('session-item-label');
            label.textContent = s.id.substring(0, 13) + '...'; // Short friendly label
            btn.appendChild(label);

            const btnDel = document.createElement('span');
            btnDel.classList.add('session-item-delete');
            btnDel.textContent = '✕';
            btnDel.addEventListener('click', (e) => {
                e.stopPropagation();
                deleteSession(s.id);
            });
            btn.appendChild(btnDel);

            btn.addEventListener('click', () => {
                selectSession(s.id);
            });
            listContainer.appendChild(btn);
        });

        // Set the active session to the first one if not set
        if (!currentSessionId && data.length > 0) {
            selectSession(data[0].id);
        }
    } catch (err) {
        console.error('Failed to load session list', err);
    }
}

async function createSession() {
    try {
        const response = await fetch('/api/sessions/create', { method: 'POST' });
        const data = await response.json();
        currentSessionId = data.id;
        
        // Refresh sidebar lists
        await loadSessions();
        // Clear chat view
        container.innerHTML = '<div class="message agent">New session started! How can I assist you today?</div>';
    } catch (err) {
        console.error('Failed to create new session', err);
    }
}

async function selectSession(id) {
    currentSessionId = id;
    
    // Update visual active classes in sidebar
    const items = document.querySelectorAll('.session-item');
    items.forEach(item => {
        if (item.getAttribute('data-id') === id) {
            item.classList.add('active');
        } else {
            item.classList.remove('active');
        }
    });

    try {
        const response = await fetch(`/api/sessions/get?id=${id}`);
        const data = await response.json();
        
        container.innerHTML = '';
        if (data.messages && data.messages.length > 0) {
            data.messages.forEach(msg => {
                appendMessage(msg.content, msg.role);
            });
        } else {
            container.innerHTML = '<div class="message agent">Empty session. Ask me anything!</div>';
        }
        container.scrollTop = container.scrollHeight;
    } catch (err) {
        console.error('Failed to fetch session messages', err);
    }
}

async function deleteSession(id) {
    if (!confirm('Are you sure you want to delete this chat session thread?')) {
        return;
    }

    try {
        await fetch(`/api/sessions/delete?id=${id}`, { method: 'DELETE' });
        if (id === currentSessionId) {
            currentSessionId = '';
        }
        await loadSessions();
    } catch (err) {
        console.error('Failed to delete session', err);
    }
}

// Hook New Session Button
document.getElementById('btn-new-session').addEventListener('click', createSession);

// Initialize Sessions at Startup
loadSessions();
