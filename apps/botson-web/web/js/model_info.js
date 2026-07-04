/**
 * model_info.js — Fetches and renders the model info strip in the chat nav bar.
 *
 * Only populates data for the OpenRouter provider; for others the strip
 * stays hidden (context_length will be -1).
 */

import { getModelInfo } from './api.js';

const strip    = document.getElementById('model-info-strip');
const nameEl   = document.getElementById('model-info-name');
const barFill  = document.getElementById('model-ctx-bar-fill');
const labelEl  = document.getElementById('model-ctx-label');

let refreshTimer = null;

/**
 * Format a token count as a human-readable string, e.g. 127k.
 * @param {number} n
 * @returns {string}
 */
function fmtTokens(n) {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000)     return Math.round(n / 1_000) + 'k';
    return String(n);
}

/**
 * Fetch /api/model-info and update the strip UI.
 */
export async function refreshModelInfo() {
    try {
        const data = await getModelInfo();

        // Only show for OpenRouter — Gemini doesn't surface context length here.
        if (data.provider !== 'openrouter' || !data.model) {
            strip.style.display = 'none';
            return;
        }

        strip.style.display = 'flex';
        nameEl.textContent  = data.model;

        const contextLength = data.context_length;   // -1 = unknown
        const promptTokens  = data.prompt_tokens;    // 0 before first message

        if (contextLength > 0) {
            const pct     = Math.min(100, Math.round((promptTokens / contextLength) * 100));
            const usedStr = fmtTokens(promptTokens);
            const maxStr  = fmtTokens(contextLength);

            barFill.style.width = pct + '%';
            labelEl.textContent = `${usedStr} / ${maxStr}`;

            // Colour-code the bar
            barFill.classList.remove('warn', 'critical');
            if (pct >= 85) {
                barFill.classList.add('critical');
            } else if (pct >= 60) {
                barFill.classList.add('warn');
            }

            // Update the tooltip on the ctx pill
            const ctxEl = document.getElementById('model-info-ctx');
            if (ctxEl) {
                ctxEl.title = `Context used: ${promptTokens.toLocaleString()} / ${contextLength.toLocaleString()} tokens (${pct}%)`;
            }
        } else {
            // Unknown context window
            barFill.style.width = '0%';
            labelEl.textContent = data.prompt_tokens > 0
                ? `${fmtTokens(data.prompt_tokens)} tokens`
                : '— / —';
        }
    } catch (err) {
        // Silent — don't break the chat UI if this endpoint fails
        console.error('Failed to refresh model info', err);
    }
}

/**
 * Start auto-refreshing model info every N seconds.
 * The refresh interval is deliberately short right after a chat completes
 * so that the token count updates promptly.
 * @param {number} intervalMs  Default 8 000 ms
 */
export function startModelInfoPolling(intervalMs = 8000) {
    stopModelInfoPolling();
    refreshModelInfo(); // immediate
    refreshTimer = setInterval(refreshModelInfo, intervalMs);
}

export function stopModelInfoPolling() {
    if (refreshTimer !== null) {
        clearInterval(refreshTimer);
        refreshTimer = null;
    }
}
