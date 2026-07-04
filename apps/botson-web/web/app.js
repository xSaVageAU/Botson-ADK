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
        updateContainmentBadge(d);
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
let currentSessionUserId = 'user';

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
            body: JSON.stringify({ message: text, session_id: currentSessionId, user_id: currentSessionUserId })
        });

        typingIndicator.remove();

        const reader = response.body.getReader();
        const decoder = new TextDecoder('utf-8');
        let buffer = '';

        let currentAgentMessageDiv = null;
        let currentAgentText = '';

        while (true) {
            const { value, done } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop(); // Keep incomplete line in buffer

            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed || !trimmed.startsWith('data: ')) continue;
                
                try {
                    const chunk = JSON.parse(trimmed.slice(6));
                    
                    if (chunk.error) {
                        appendMessage('❌ Error: ' + chunk.error, 'agent');
                        continue;
                    }

                    if (chunk.done) {
                        break;
                    }

                    // Handle tool calls
                    if (chunk.tool_calls && chunk.tool_calls.length > 0) {
                        for (const tc of chunk.tool_calls) {
                            appendMessage(tc.args, 'tool_call', tc.name);
                        }
                        currentAgentMessageDiv = null; // Reset so subsequent text is in a new bubble
                    }

                    // Handle tool responses
                    if (chunk.tool_response) {
                        appendMessage(chunk.tool_response.output, 'tool_response', chunk.tool_response.name);
                        currentAgentMessageDiv = null; // Reset so subsequent text is in a new bubble
                    }

                    // Handle streamed model text response
                    if (chunk.text) {
                        if (!currentAgentMessageDiv) {
                            currentAgentMessageDiv = document.createElement('div');
                            currentAgentMessageDiv.classList.add('message', 'agent');
                            container.appendChild(currentAgentMessageDiv);
                            currentAgentText = '';
                        }
                        currentAgentText += chunk.text;
                        
                        renderAgentText(currentAgentMessageDiv, currentAgentText);
                        container.scrollTop = container.scrollHeight;
                    }
                } catch (err) {
                    console.error('Error parsing SSE line', err);
                }
            }
        }

        // Refresh fully resolved session messages from DB
        await selectSession(currentSessionId, currentSessionUserId);
        await loadSessions();
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

function escapeHTML(str) {
    if (!str) return '';
    return str
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');
}

function renderAgentText(div, text) {
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
}

function appendMessage(text, sender, toolName) {
    const div = document.createElement('div');
    div.classList.add('message', sender);

    if (sender === 'agent') {
        renderAgentText(div, text);
    } else if (sender === 'tool_call') {
        div.innerHTML = `<span style="opacity:0.8;">⚡</span> call: ${toolName}(${escapeHTML(text)})`;
    } else if (sender === 'tool_response') {
        div.innerHTML = `
            <details>
                <summary><span style="opacity:0.8;color:var(--success);">✔</span> response (${toolName})</summary>
                <pre>${escapeHTML(text)}</pre>
            </details>
        `;
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
 * Render sessions as cards.
 */
async function loadSessions() {
    const bar = document.getElementById('session-list');
    if (!bar) return;

    // Clear all session cards
    bar.innerHTML = '';

    try {
        const response = await fetch('/api/sessions');
        const data     = await response.json();

        if (!data || data.length === 0) {
            createSession();
            return;
        }

        data.forEach(s => {
            const card = document.createElement('div');
            card.classList.add('session-card');
            card.setAttribute('role', 'listitem');
            if (s.id === currentSessionId) card.classList.add('active');
            card.setAttribute('data-id', s.id);
            card.setAttribute('data-user-id', s.user_id);

            // Title format
            let titleText = s.id;
            if (s.id.startsWith("discord:")) {
                const parts = s.id.split("-");
                const channelId = parts[0].replace("discord:", "");
                titleText = "Discord: " + channelId.substring(0, 8);
            } else {
                titleText = "Web Chat: " + s.id.substring(0, 8);
            }

            const titleEl = document.createElement('div');
            titleEl.classList.add('session-card-title');
            titleEl.textContent = titleText;
            card.appendChild(titleEl);

            const previewEl = document.createElement('div');
            previewEl.classList.add('session-card-preview');
            previewEl.textContent = s.preview || "New Conversation";
            card.appendChild(previewEl);

            const delBtn = document.createElement('span');
            delBtn.classList.add('session-card-del');
            delBtn.setAttribute('role', 'button');
            delBtn.setAttribute('aria-label', 'Delete session');
            delBtn.textContent = '✕';
            delBtn.addEventListener('click', (e) => {
                e.stopPropagation();
                deleteSession(s.id, s.user_id);
            });
            card.appendChild(delBtn);

            card.addEventListener('click', () => selectSession(s.id, s.user_id));
            bar.appendChild(card);
        });

        if (!currentSessionId && data.length > 0) {
            selectSession(data[0].id, data[0].user_id);
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
        currentSessionUserId = 'user';
        await loadSessions();
        container.innerHTML = '<div class="message agent">New session started! How can I assist you today?</div>';
    } catch (err) {
        console.error('Failed to create session', err);
    }
}

async function selectSession(id, userId) {
    currentSessionId = id;
    currentSessionUserId = userId || 'user';

    document.querySelectorAll('.session-card').forEach(card => {
        card.classList.toggle('active', card.getAttribute('data-id') === id);
    });

    try {
        const response = await fetch(`/api/sessions/get?id=${id}&user_id=${currentSessionUserId}`);
        const data     = await response.json();

        container.innerHTML = '';
        if (data.messages && data.messages.length > 0) {
            data.messages.forEach(msg => appendMessage(msg.content, msg.role, msg.tool_name));
        } else {
            container.innerHTML = '<div class="message agent">Empty session. Ask me anything!</div>';
        }
        container.scrollTop = container.scrollHeight;
    } catch (err) {
        console.error('Failed to fetch session messages', err);
    }
}

async function deleteSession(id, userId) {
    if (!confirm('Delete this chat session?')) return;
    try {
        await fetch(`/api/sessions/delete?id=${id}&user_id=${userId || 'user'}`, { method: 'DELETE' });
        if (id === currentSessionId) {
            currentSessionId = '';
            currentSessionUserId = 'user';
        }
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

let setupPollingInterval = null;

function updateContainmentBadge(data) {
    const badgeEl = document.getElementById('containment-badge');
    const badgeLabelEl = document.getElementById('containment-label');
    if (!badgeEl) return;
    
    if (data.sandboxing_enabled) {
        badgeEl.style.display = 'flex';
        if (data.active_target === 'sandbox') {
            badgeEl.style.background = 'var(--warning-bg)';
            badgeEl.style.borderColor = 'rgba(251, 191, 36, 0.20)';
            badgeEl.style.color = 'var(--warning)';
            badgeEl.querySelector('.status-pill-dot').style.background = 'var(--warning)';
            badgeLabelEl.textContent = 'Sandbox';
        } else {
            badgeEl.style.background = 'rgba(124, 136, 153, 0.08)';
            badgeEl.style.borderColor = 'rgba(124, 136, 153, 0.20)';
            badgeEl.style.color = 'var(--text-secondary)';
            badgeEl.querySelector('.status-pill-dot').style.background = 'var(--text-secondary)';
            badgeLabelEl.textContent = 'Host';
        }
    } else {
        badgeEl.style.display = 'none';
    }
}

function startSetupPolling() {
    const logsContainer = document.getElementById('setup-logs-container');
    const logsEl = document.getElementById('setup-logs');
    logsContainer.style.display = 'block';

    if (setupPollingInterval) clearInterval(setupPollingInterval);

    setupPollingInterval = setInterval(async () => {
        try {
            const res = await fetch('/api/sandbox/setup/status');
            const data = await res.json();

            if (data.logs && data.logs.length > 0) {
                logsEl.textContent = data.logs.join('\n');
                logsContainer.scrollTop = logsContainer.scrollHeight;
            }

            if (!data.running) {
                clearInterval(setupPollingInterval);
                setupPollingInterval = null;
                const btnWslSetup = document.getElementById('btn-wsl-setup');
                btnWslSetup.disabled = false;
                loadSandbox();

                if (data.success) {
                    showToast('Sandbox setup finished successfully!', 'success');
                } else if (data.error) {
                    showToast(`Sandbox setup failed: ${data.error}`, 'error');
                }
            }
        } catch (err) {
            console.error('Error polling setup status', err);
        }
    }, 1500);
}

async function loadSandbox() {
    try {
        const response = await fetch('/api/sandbox');
        const data     = await response.json();

        const isWindows = (data.os === 'windows');

        // Dynamic OS adaptations
        const statusLabelEl = document.querySelector('#screen-sandbox .status-card:nth-child(1) .status-card-label');
        const statusDescEl = document.querySelector('#screen-sandbox .status-card:nth-child(1) .desc');
        const provisionLabelEl = document.querySelector('#screen-sandbox .status-card:nth-child(2) .status-card-label');
        const provisionDescEl = document.querySelector('#screen-sandbox .status-card:nth-child(2) .desc');
        const setupHeaderTitle = document.getElementById('setup-header-title');
        const setupPanelDesc = document.getElementById('setup-panel-desc');
        const btnWslSetup = document.getElementById('btn-wsl-setup');

        if (isWindows) {
            statusLabelEl.textContent = 'Sandboxing Status';
            statusDescEl.textContent = 'Is WSL filesystem containment currently active?';
            provisionLabelEl.textContent = 'WSL Provision';
            provisionDescEl.textContent = 'Has the isolated WSL instance been provisioned?';
            setupHeaderTitle.textContent = 'WSL Setup Utilities';
            setupPanelDesc.textContent = 'If WSL is not yet provisioned, trigger the automatic downloader and setup script here.';
            if (!btnWslSetup.disabled) {
                btnWslSetup.textContent = 'Install & Provision WSL Sandbox';
            }
        } else {
            statusLabelEl.textContent = 'Sandboxing Status';
            statusDescEl.textContent = 'Is gVisor filesystem containment currently active?';
            provisionLabelEl.textContent = 'gVisor Setup';
            provisionDescEl.textContent = 'Has the local gVisor (runsc) binary been configured?';
            setupHeaderTitle.textContent = 'gVisor Setup Utilities';
            setupPanelDesc.textContent = 'If gVisor (runsc) is not yet configured, trigger the automatic downloader and setup script here.';
            if (!btnWslSetup.disabled) {
                btnWslSetup.textContent = 'Install & Configure gVisor Sandbox';
            }
        }

        const statusEl = document.getElementById('sb-status-text');
        statusEl.textContent = data.sandboxing_enabled ? 'Active' : 'Disabled';
        statusEl.className   = 'status-value ' + (data.sandboxing_enabled ? 'online' : 'offline');

        const wslEl = document.getElementById('sb-wsl-text');
        wslEl.textContent = data.setup_done ? 'Provisioned' : 'Not Installed';
        wslEl.className   = 'status-value ' + (data.setup_done ? 'online' : 'offline');

        document.getElementById('sb-cache-text').textContent = data.cache_dir || 'N/A';

        // Toggle Reset/Test Actions row based on setup_done
        const actionsRow = document.getElementById('sandbox-util-actions');
        if (data.setup_done) {
            actionsRow.style.display = 'flex';
        } else {
            actionsRow.style.display = 'none';
        }

        updateContainmentBadge(data);
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
        showToast(data.message || 'Sandbox setup triggered in background.', 'success');
        startSetupPolling();
    } catch (err) {
        showToast('Failed to initiate sandbox setup.', 'error');
        btnWslSetup.disabled = false;
        loadSandbox();
    }
});

const btnSandboxTest = document.getElementById('btn-sandbox-test');
btnSandboxTest.addEventListener('click', async () => {
    btnSandboxTest.disabled = true;
    btnSandboxTest.textContent = 'Testing Containment…';
    try {
        const response = await fetch('/api/sandbox/test', { method: 'POST' });
        const data = await response.json();
        if (data.status === 'success') {
            showToast(`Test Succeeded!\nOutput: ${data.output}`, 'success');
        } else {
            showToast(data.message || 'Test failed.', 'error');
        }
    } catch (err) {
        showToast('Failed to execute sandbox containment test.', 'error');
    } finally {
        btnSandboxTest.disabled = false;
        btnSandboxTest.textContent = 'Test Containment';
    }
});

const btnSandboxReset = document.getElementById('btn-sandbox-reset');
btnSandboxReset.addEventListener('click', async () => {
    if (!confirm('Are you sure you want to completely wipe and reset the sandbox workspace? All modifications inside the sandbox will be lost.')) {
        return;
    }
    btnSandboxReset.disabled = true;
    btnSandboxReset.textContent = 'Resetting Workspace…';
    try {
        const response = await fetch('/api/sandbox/reset', { method: 'POST' });
        const data = await response.json();
        if (data.status === 'success') {
            showToast('Sandbox workspace reset successfully.', 'success');
        } else {
            showToast(data.message || 'Reset failed.', 'error');
        }
    } catch (err) {
        showToast('Failed to reset sandbox workspace.', 'error');
    } finally {
        btnSandboxReset.disabled = false;
        btnSandboxReset.textContent = 'Reset Workspace';
        loadSandbox();
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
