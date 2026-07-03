// ─── Page Stack Engine ────────────────────────────────────────────────────────

/**
 * Simple history-like page stack.
 * Only "home" is the base; all others push on top of it.
 */
let currentScreen = 'screen-home';

/**
 * Navigate to a screen (push).
 * @param {string} targetId  - The element ID of the destination screen.
 * @param {Function} [onEnter] - Optional callback fired after the screen becomes active.
 */
function navigateTo(targetId, onEnter) {
    if (targetId === currentScreen) return;

    const prev = document.getElementById(currentScreen);
    const next = document.getElementById(targetId);
    if (!prev || !next) return;

    // Push current screen back
    prev.classList.remove('active');
    prev.classList.add('pushed');

    // Bring next screen in from the right
    next.classList.remove('pushed');
    next.classList.add('active');

    currentScreen = targetId;
    if (onEnter) {
        // Wait for the CSS transition to mostly complete before running
        setTimeout(onEnter, 120);
    }
}

/**
 * Navigate back to the home screen (pop).
 */
function navigateBack() {
    if (currentScreen === 'screen-home') return;

    const current = document.getElementById(currentScreen);
    const home    = document.getElementById('screen-home');

    current.classList.remove('active');
    // Slide current screen out to the right
    current.style.transition = 'transform 360ms cubic-bezier(0.4, 0, 0.2, 1), opacity 360ms cubic-bezier(0.4, 0, 0.2, 1)';
    current.style.transform  = 'translateX(100%)';
    current.style.opacity    = '0';

    home.classList.remove('pushed');
    home.classList.add('active');
    currentScreen = 'screen-home';

    // Reset the outgoing screen after transition
    setTimeout(() => {
        current.style.transition = '';
        current.style.transform  = '';
        current.style.opacity    = '';
        current.classList.remove('active', 'pushed');
    }, 380);

    // Refresh home stats
    refreshHomeStats();
}

// Wire up all app cards on the home screen
document.querySelectorAll('.app-card[data-target]').forEach(card => {
    card.addEventListener('click', () => {
        const target = card.getAttribute('data-target');

        // Decide what to fetch when entering each screen
        const enterCallbacks = {
            'screen-config':   loadConfig,
            'screen-sandbox':  loadSandbox,
            'screen-pairings': loadPairings,
        };

        navigateTo(target, enterCallbacks[target]);
    });
});

// Wire up all back buttons
document.querySelectorAll('[data-back]').forEach(btn => {
    btn.addEventListener('click', navigateBack);
});

// ─── Home Stats ───────────────────────────────────────────────────────────────

async function refreshHomeStats() {
    // Sessions count
    try {
        const r = await fetch('/api/sessions');
        const d = await r.json();
        const el = document.getElementById('stat-sessions');
        const count = Array.isArray(d) ? d.length : 0;
        el.textContent = count;
        el.className = 'stat-value ' + (count > 0 ? 'ok' : 'dim');
    } catch { /* silent */ }

    // Sandbox status
    try {
        const r = await fetch('/api/sandbox');
        const d = await r.json();
        const el = document.getElementById('stat-sandbox');
        el.textContent = d.sandboxing_enabled ? 'On' : 'Off';
        el.className   = 'stat-value ' + (d.sandboxing_enabled ? 'ok' : 'dim');
    } catch { /* silent */ }

    // Pending pairings count
    try {
        const r = await fetch('/api/pairings');
        const d = await r.json();
        const el = document.getElementById('stat-pairings');
        const count = Array.isArray(d) ? d.length : 0;
        el.textContent = count;
        el.className   = 'stat-value ' + (count > 0 ? 'warn' : 'dim');
    } catch { /* silent */ }
}

// ─── Toast Notifications ──────────────────────────────────────────────────────

/**
 * @param {string} message
 * @param {'success'|'error'|'info'} type
 * @param {number} durationMs
 */
function showToast(message, type = 'info', durationMs = 4000) {
    const container = document.getElementById('toast-container');
    const toast = document.createElement('div');
    toast.classList.add('toast', type);

    const icons = { success: '✓', error: '✕', info: 'ℹ' };
    toast.innerHTML = `<span class="toast-icon">${icons[type] ?? '•'}</span><span>${message}</span>`;
    container.appendChild(toast);

    setTimeout(() => {
        toast.style.animation = 'toastOut 300ms cubic-bezier(0.4, 0, 1, 1) forwards';
        toast.addEventListener('animationend', () => toast.remove(), { once: true });
    }, durationMs);
}

// ─── Chat Client ──────────────────────────────────────────────────────────────

let currentSessionId = '';

const form      = document.getElementById('input-form');
const input     = document.getElementById('message-input');
const container = document.getElementById('chat-container');
const sendBtn   = document.getElementById('send-btn');

form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || !currentSessionId) return;

    input.value      = '';
    input.disabled   = true;
    sendBtn.disabled = true;

    appendMessage(text, 'user');

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
        input.disabled   = false;
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
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/`([^`\n]+)`/g, '<code>$1</code>');

        const codeBlocks = formatted.split('```');
        let result = '';
        for (let i = 0; i < codeBlocks.length; i++) {
            if (i % 2 === 1) {
                const lines = codeBlocks[i].split('\n');
                const lang  = lines[0];
                const code  = lines.slice(1).join('\n').trim();
                result += `<pre><code class="language-${lang}">${code}</code></pre>`;
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

// ─── Session Management ───────────────────────────────────────────────────────

/**
 * Render sessions as horizontal pill buttons in the chat session bar.
 */
async function loadSessions() {
    const bar = document.getElementById('session-list');

    // Keep the new-session button at the end
    const newBtn = document.getElementById('btn-new-session');

    // Remove all session pills (not the new-session button)
    bar.querySelectorAll('.session-pill').forEach(p => p.remove());

    try {
        const response = await fetch('/api/sessions');
        const data     = await response.json();

        if (!data || data.length === 0) {
            createSession();
            return;
        }

        // Insert pills before the new-session button
        data.forEach(s => {
            const pill = document.createElement('div');
            pill.classList.add('session-pill');
            pill.setAttribute('role', 'listitem');
            if (s.id === currentSessionId) pill.classList.add('active');
            pill.setAttribute('data-id', s.id);

            const label = document.createElement('span');
            label.textContent = s.id.substring(0, 10) + '…';
            pill.appendChild(label);

            const delSpan = document.createElement('span');
            delSpan.classList.add('session-pill-del');
            delSpan.setAttribute('role', 'button');
            delSpan.setAttribute('aria-label', 'Delete session');
            delSpan.textContent = '✕';
            delSpan.addEventListener('click', (e) => {
                e.stopPropagation();
                deleteSession(s.id);
            });
            pill.appendChild(delSpan);

            pill.addEventListener('click', () => selectSession(s.id));
            bar.insertBefore(pill, newBtn);
        });

        if (!currentSessionId && data.length > 0) {
            selectSession(data[0].id);
        }
    } catch (err) {
        console.error('Failed to load sessions', err);
    }
}

async function createSession() {
    try {
        const response = await fetch('/api/sessions/create', { method: 'POST' });
        const data     = await response.json();
        currentSessionId = data.id;
        await loadSessions();
        container.innerHTML = '<div class="message agent">New session started! How can I assist you today?</div>';
    } catch (err) {
        console.error('Failed to create session', err);
    }
}

async function selectSession(id) {
    currentSessionId = id;

    document.querySelectorAll('.session-pill').forEach(pill => {
        pill.classList.toggle('active', pill.getAttribute('data-id') === id);
    });

    try {
        const response = await fetch(`/api/sessions/get?id=${id}`);
        const data     = await response.json();

        container.innerHTML = '';
        if (data.messages && data.messages.length > 0) {
            data.messages.forEach(msg => appendMessage(msg.content, msg.role));
        } else {
            container.innerHTML = '<div class="message agent">Empty session. Ask me anything!</div>';
        }
        container.scrollTop = container.scrollHeight;
    } catch (err) {
        console.error('Failed to fetch session messages', err);
    }
}

async function deleteSession(id) {
    if (!confirm('Delete this chat session?')) return;
    try {
        await fetch(`/api/sessions/delete?id=${id}`, { method: 'DELETE' });
        if (id === currentSessionId) currentSessionId = '';
        await loadSessions();
    } catch (err) {
        console.error('Failed to delete session', err);
    }
}

document.getElementById('btn-new-session').addEventListener('click', createSession);

// ─── Configuration ────────────────────────────────────────────────────────────

async function loadConfig() {
    try {
        const response = await fetch('/api/config');
        const data     = await response.json();

        document.getElementById('cfg-provider').value      = data.provider || 'openrouter';
        document.getElementById('cfg-instruction').value   = data.instruction || '';
        document.getElementById('cfg-discord-token').value = data.discord_token_masked || '';

        document.getElementById('cfg-sandboxing').checked  = data.sandboxing || false;
        document.getElementById('cfg-coder').checked       = data.coder      || false;

        document.getElementById('cfg-or-model').value      = data.openrouter_model    || '';
        document.getElementById('cfg-or-key').value        = data.openrouter_key_mask || '';

        document.getElementById('cfg-gemini-model').value  = data.gemini_model    || '';
        document.getElementById('cfg-gemini-key').value    = data.gemini_key_mask || '';
    } catch (err) {
        console.error('Failed to load configuration', err);
        showToast('Failed to load configuration.', 'error');
    }
}

document.getElementById('config-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const payload = {
        provider:         document.getElementById('cfg-provider').value,
        instruction:      document.getElementById('cfg-instruction').value,
        discord_token:    document.getElementById('cfg-discord-token').value,
        sandboxing:       document.getElementById('cfg-sandboxing').checked,
        coder:            document.getElementById('cfg-coder').checked,
        openrouter_model: document.getElementById('cfg-or-model').value,
        openrouter_key:   document.getElementById('cfg-or-key').value,
        gemini_model:     document.getElementById('cfg-gemini-model').value,
        gemini_key:       document.getElementById('cfg-gemini-key').value,
    };

    try {
        const response = await fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        const data = await response.json();
        showToast(data.message || 'Configuration saved!', 'success');
        loadConfig();
    } catch (err) {
        showToast('Failed to save configuration.', 'error');
    }
});

// ─── Sandbox ──────────────────────────────────────────────────────────────────

async function loadSandbox() {
    try {
        const response = await fetch('/api/sandbox');
        const data     = await response.json();

        const statusEl = document.getElementById('sb-status-text');
        statusEl.textContent = data.sandboxing_enabled ? 'Active' : 'Disabled';
        statusEl.className   = 'status-value ' + (data.sandboxing_enabled ? 'online' : 'offline');

        const wslEl = document.getElementById('sb-wsl-text');
        wslEl.textContent = data.wsl_installed ? 'Provisioned' : 'Not Installed';
        wslEl.className   = 'status-value ' + (data.wsl_installed ? 'online' : 'offline');

        document.getElementById('sb-cache-text').textContent = data.cache_dir || 'N/A';
    } catch (err) {
        console.error('Failed to load sandbox status', err);
    }
}

const btnWslSetup = document.getElementById('btn-wsl-setup');
btnWslSetup.addEventListener('click', async () => {
    btnWslSetup.disabled    = true;
    btnWslSetup.textContent = 'Installing / Provisioning…';
    try {
        const response = await fetch('/api/sandbox/setup', { method: 'POST' });
        const data     = await response.json();
        showToast(data.message || 'WSL setup triggered in background.', 'success');
    } catch (err) {
        showToast('Failed to initiate WSL setup.', 'error');
    } finally {
        setTimeout(loadSandbox, 5000);
        btnWslSetup.disabled    = false;
        btnWslSetup.textContent = 'Install & Provision WSL Sandbox';
    }
});

// ─── Pairings ─────────────────────────────────────────────────────────────────

async function loadPairings() {
    const tbody = document.getElementById('pairings-tbody');
    try {
        const response = await fetch('/api/pairings');
        const data     = await response.json();

        tbody.innerHTML = '';
        if (!data || data.length === 0) {
            tbody.innerHTML = `
                <tr><td colspan="5" class="empty-state">
                    <span class="empty-state-icon" aria-hidden="true">⊘</span>
                    No pending pairing requests.
                </td></tr>`;
            return;
        }

        data.forEach(p => {
            const tr = document.createElement('tr');

            const cols = [p.gateway, p.user_id, p.username, p.code];
            cols.forEach(text => {
                const td = document.createElement('td');
                td.textContent = text;
                tr.appendChild(td);
            });

            const tdActions  = document.createElement('td');
            const btnApprove = document.createElement('button');
            btnApprove.textContent = 'Approve';
            btnApprove.classList.add('approve-btn');
            btnApprove.addEventListener('click', () => approvePairing(p.gateway, p.code));
            tdActions.appendChild(btnApprove);
            tr.appendChild(tdActions);

            tbody.appendChild(tr);
        });
    } catch (err) {
        tbody.innerHTML = `
            <tr><td colspan="5" class="empty-state">
                <span class="empty-state-icon" aria-hidden="true">⊘</span>
                Failed to load pairings.
            </td></tr>`;
    }
}

async function approvePairing(gateway, code) {
    if (!confirm(`Approve authorization code "${code}" on gateway "${gateway}"?`)) return;

    try {
        const response = await fetch('/api/pairings/approve', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ gateway, code })
        });
        const data = await response.json();
        if (data.status === 'success') {
            showToast(data.message, 'success');
            loadPairings();
        } else {
            showToast('Error: ' + data.message, 'error');
        }
    } catch (err) {
        showToast('Failed to send pairing approval.', 'error');
    }
}

// ─── Initialisation ───────────────────────────────────────────────────────────

// Load sessions so chat is ready when user enters it
loadSessions();

// Populate home stats
refreshHomeStats();
