/**
 * chat.js — Chat screen logic: session list, message rendering, SSE streaming.
 */

import { showToast, escapeHTML } from './ui.js';
import {
    getSessions,
    createSession as apiCreateSession,
    deleteSession as apiDeleteSession,
    getSession,
    sendChatMessage,
} from './api.js';

// Lazy-loaded to avoid a circular import — main.js re-exports this.
let _refreshModelInfo = null;
export function setModelInfoRefresher(fn) { _refreshModelInfo = fn; }

// ─── Module State ─────────────────────────────────────────────────────────────

let currentSessionId = '';
let currentSessionUserId = 'user';

// ─── DOM References ───────────────────────────────────────────────────────────

const chatContainer = document.getElementById('chat-container');
const form          = document.getElementById('input-form');
const input         = document.getElementById('message-input');
const sendBtn       = document.getElementById('send-btn');

// ─── Message Rendering ────────────────────────────────────────────────────────

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

    chatContainer.appendChild(div);
}

function showTypingIndicator() {
    const div = document.createElement('div');
    div.classList.add('typing-indicator');
    div.innerHTML = '<span></span><span></span><span></span>';
    chatContainer.appendChild(div);
    return div;
}

// ─── Session Management ───────────────────────────────────────────────────────

const HERO_HTML = `
    <div class="chat-hero-area">
        <div class="hero-logo">🤖</div>
        <h2 class="hero-title">Where should we begin?</h2>
        <p class="hero-subtitle">Ask Botson to write code, manage configurations, or execute commands in your sandbox environment.</p>
    </div>
`;

/**
 * Fetch and render the session sidebar list.
 * If no sessions exist, creates a fresh one automatically.
 */
export async function loadSessions() {
    const bar = document.getElementById('session-list');
    if (!bar) return;

    bar.innerHTML = '';

    try {
        const data = await getSessions();

        if (!data || data.length === 0) {
            await createSession();
            return;
        }

        data.forEach(s => {
            const card = document.createElement('div');
            card.classList.add('session-card');
            card.setAttribute('role', 'listitem');
            if (s.id === currentSessionId) card.classList.add('active');
            card.setAttribute('data-id', s.id);
            card.setAttribute('data-user-id', s.user_id);

            const sessionDate = new Date(s.last_update);
            const timeStr = sessionDate.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

            let titleText;
            if (s.id.startsWith('discord:')) {
                const parts    = s.id.split('-');
                const channelId = parts[0].replace('discord:', '');
                titleText = 'Discord: ' + channelId.substring(0, 6) + ' (' + timeStr + ')';
            } else {
                titleText = 'Web Chat: ' + s.id.substring(0, 6) + ' (' + timeStr + ')';
            }

            const titleEl = document.createElement('div');
            titleEl.classList.add('session-card-title');
            titleEl.textContent = titleText;
            card.appendChild(titleEl);

            const previewEl = document.createElement('div');
            previewEl.classList.add('session-card-preview');
            previewEl.textContent = s.preview || 'New Conversation';
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

/**
 * Create a new session and display the greeting hero screen.
 */
export async function createSession() {
    try {
        const data = await apiCreateSession();
        currentSessionId     = data.id;
        currentSessionUserId = 'user';
        await loadSessions();
        chatContainer.innerHTML = HERO_HTML;
    } catch (err) {
        console.error('Failed to create session', err);
    }
}

/**
 * Select a session and load its message history.
 * @param {string} id
 * @param {string} userId
 */
export async function selectSession(id, userId) {
    currentSessionId     = id;
    currentSessionUserId = userId || 'user';

    document.querySelectorAll('.session-card').forEach(card => {
        card.classList.toggle('active', card.getAttribute('data-id') === id);
    });

    try {
        const data = await getSession(id, currentSessionUserId);

        chatContainer.innerHTML = '';
        if (data.messages && data.messages.length > 0) {
            data.messages.forEach(msg => appendMessage(msg.content, msg.role, msg.tool_name));
        } else {
            chatContainer.innerHTML = HERO_HTML;
        }
        chatContainer.scrollTop = chatContainer.scrollHeight;
    } catch (err) {
        console.error('Failed to fetch session messages', err);
    }
}

/**
 * Delete a session after user confirmation.
 * @param {string} id
 * @param {string} userId
 */
export async function deleteSession(id, userId) {
    if (!confirm('Delete this chat session?')) return;
    try {
        await apiDeleteSession(id, userId);
        if (id === currentSessionId) {
            currentSessionId     = '';
            currentSessionUserId = 'user';
        }
        await loadSessions();
    } catch (err) {
        console.error('Failed to delete session', err);
    }
}

// ─── Chat Form / SSE Stream ───────────────────────────────────────────────────

/**
 * Wire up the chat submit handler. Called once from main.js on DOMContentLoaded.
 */
export function initChatForm() {
    form.addEventListener('submit', async (e) => {
        e.preventDefault();
        const text = input.value.trim();
        if (!text || !currentSessionId) return;

        input.value      = '';
        input.disabled   = true;
        sendBtn.disabled = true;

        appendMessage(text, 'user');

        const typingIndicator = showTypingIndicator();
        chatContainer.scrollTop = chatContainer.scrollHeight;

        try {
            const reader  = await sendChatMessage(text, currentSessionId, currentSessionUserId);
            typingIndicator.remove();

            const decoder = new TextDecoder('utf-8');
            let buffer    = '';

            let currentAgentMessageDiv = null;
            let currentAgentText       = '';

            while (true) {
                const { value, done } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop();

                for (const line of lines) {
                    const trimmed = line.trim();
                    if (!trimmed || !trimmed.startsWith('data: ')) continue;

                    try {
                        const chunk = JSON.parse(trimmed.slice(6));

                        if (chunk.error) {
                            appendMessage('❌ Error: ' + chunk.error, 'agent');
                            continue;
                        }
                        if (chunk.done) break;

                        if (chunk.tool_calls && chunk.tool_calls.length > 0) {
                            for (const tc of chunk.tool_calls) {
                                appendMessage(tc.args, 'tool_call', tc.name);
                            }
                            currentAgentMessageDiv = null;
                        }

                        if (chunk.tool_response) {
                            appendMessage(chunk.tool_response.output, 'tool_response', chunk.tool_response.name);
                            currentAgentMessageDiv = null;
                        }

                        if (chunk.text) {
                            if (!currentAgentMessageDiv) {
                                currentAgentMessageDiv = document.createElement('div');
                                currentAgentMessageDiv.classList.add('message', 'agent');
                                chatContainer.appendChild(currentAgentMessageDiv);
                                currentAgentText = '';
                            }
                            currentAgentText += chunk.text;
                            renderAgentText(currentAgentMessageDiv, currentAgentText);
                            chatContainer.scrollTop = chatContainer.scrollHeight;
                        }
                    } catch (err) {
                        console.error('Error parsing SSE line', err);
                    }
                }
            }

            // Refresh fully resolved session messages from DB
            await selectSession(currentSessionId, currentSessionUserId);
            await loadSessions();
            // Update the model info / context bar
            if (_refreshModelInfo) _refreshModelInfo();
        } catch (err) {
            typingIndicator.remove();
            appendMessage('❌ Connection error to the local server.', 'agent');
        } finally {
            input.disabled   = false;
            sendBtn.disabled = false;
            input.focus();
            chatContainer.scrollTop = chatContainer.scrollHeight;
        }
    });

    document.getElementById('btn-new-session').addEventListener('click', createSession);
}
