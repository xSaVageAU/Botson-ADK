/**
 * main.js — App entry point.
 *
 * Handles the page-stack navigation engine, home stats, and wires up each
 * screen module when the user enters it.
 */

import { loadSessions, initChatForm, setModelInfoRefresher } from './chat.js';
import { loadConfig, initConfigForm }  from './config.js';
import { loadSandbox, initSandboxControls, updateContainmentBadge } from './sandbox.js';
import { loadPairings } from './pairings.js';
import { getSessions, getSandboxStatus, getPairings } from './api.js';
import { startModelInfoPolling, refreshModelInfo } from './model_info.js';

export { refreshModelInfo }; // re-export so chat.js can call it after messages

// ─── Page Stack Engine ────────────────────────────────────────────────────────

let currentScreen = 'screen-home';

/**
 * Navigate to a screen (push).
 * @param {string} targetId   — Element ID of the destination screen.
 * @param {Function} [onEnter] — Optional callback fired after the transition.
 */
function navigateTo(targetId, onEnter) {
    if (targetId === currentScreen) return;

    const prev = document.getElementById(currentScreen);
    const next = document.getElementById(targetId);
    if (!prev || !next) return;

    prev.classList.remove('active');
    prev.classList.add('pushed');

    next.classList.remove('pushed');
    next.classList.add('active');

    currentScreen = targetId;
    if (onEnter) {
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
    current.style.transition = 'transform 360ms cubic-bezier(0.4, 0, 0.2, 1), opacity 360ms cubic-bezier(0.4, 0, 0.2, 1)';
    current.style.transform  = 'translateX(100%)';
    current.style.opacity    = '0';

    home.classList.remove('pushed');
    home.classList.add('active');
    currentScreen = 'screen-home';

    setTimeout(() => {
        current.style.transition = '';
        current.style.transform  = '';
        current.style.opacity    = '';
        current.classList.remove('active', 'pushed');
    }, 380);

    refreshHomeStats();
}

// ─── Home Stats ───────────────────────────────────────────────────────────────

async function refreshHomeStats() {
    // Sessions count
    try {
        const d   = await getSessions();
        const el  = document.getElementById('stat-sessions');
        const cnt = Array.isArray(d) ? d.length : 0;
        el.textContent = cnt;
        el.className   = 'stat-value ' + (cnt > 0 ? 'ok' : 'dim');
    } catch { /* silent */ }

    // Sandbox status
    try {
        const d  = await getSandboxStatus();
        const el = document.getElementById('stat-sandbox');
        el.textContent = d.sandboxing_enabled ? 'On' : 'Off';
        el.className   = 'stat-value ' + (d.sandboxing_enabled ? 'ok' : 'dim');
        updateContainmentBadge(d);
    } catch { /* silent */ }

    // Pending pairings count
    try {
        const d   = await getPairings();
        const el  = document.getElementById('stat-pairings');
        const cnt = Array.isArray(d) ? d.length : 0;
        el.textContent = cnt;
        el.className   = 'stat-value ' + (cnt > 0 ? 'warn' : 'dim');
    } catch { /* silent */ }
}

// ─── Initialisation ───────────────────────────────────────────────────────────

// Wire up navigation cards
document.querySelectorAll('.app-card[data-target]').forEach(card => {
    card.addEventListener('click', () => {
        const target = card.getAttribute('data-target');

        const enterCallbacks = {
            'screen-chat':     () => { loadSessions(); startModelInfoPolling(); },
            'screen-config':   loadConfig,
            'screen-sandbox':  loadSandbox,
            'screen-pairings': loadPairings,
        };

        navigateTo(target, enterCallbacks[target]);
    });
});

// Wire up back buttons
document.querySelectorAll('[data-back]').forEach(btn => {
    btn.addEventListener('click', navigateBack);
});

// Wire up form handlers (once, at startup)
initChatForm();
initConfigForm();
initSandboxControls();
// Pass the model info refresher into chat so it can call it post-message.
setModelInfoRefresher(refreshModelInfo);

// Pre-load sessions so chat is ready when the user enters it
loadSessions();

// Populate home stats on first load
refreshHomeStats();
