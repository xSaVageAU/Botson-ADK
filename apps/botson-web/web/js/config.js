/**
 * config.js — Configuration screen: form loading, saving, and hot-reload.
 */

import { showToast } from './ui.js';
import { getConfig, saveConfig } from './api.js';

/**
 * Fetch /api/config and populate the config form fields.
 */
export async function loadConfig() {
    try {
        const data = await getConfig();

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

/**
 * Wire up the config form submit handler. Called once from main.js.
 */
export function initConfigForm() {
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
            const data = await saveConfig(payload);
            showToast(data.message || 'Configuration saved!', 'success');
            loadConfig();
        } catch (err) {
            showToast('Failed to save configuration.', 'error');
        }
    });
}
