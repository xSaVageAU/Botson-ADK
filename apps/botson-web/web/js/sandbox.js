/**
 * sandbox.js — Sandbox screen: status loading, setup, test, and reset actions.
 */

import { showToast } from './ui.js';
import {
    getSandboxStatus,
    triggerSandboxSetup,
    getSandboxSetupStatus,
    resetSandbox,
    testSandbox,
} from './api.js';

let setupPollingInterval = null;

/**
 * Update the containment environment badge in the header.
 * @param {object} data  Response from /api/sandbox
 */
export function updateContainmentBadge(data) {
    const badgeEl     = document.getElementById('containment-badge');
    const badgeLabelEl = document.getElementById('containment-label');
    if (!badgeEl) return;

    if (data.sandboxing_enabled) {
        badgeEl.style.display = 'flex';
        if (data.active_target === 'sandbox') {
            badgeEl.style.background   = 'var(--warning-bg)';
            badgeEl.style.borderColor  = 'rgba(251, 191, 36, 0.20)';
            badgeEl.style.color        = 'var(--warning)';
            badgeEl.querySelector('.status-pill-dot').style.background = 'var(--warning)';
            badgeLabelEl.textContent   = 'Sandbox';
        } else {
            badgeEl.style.background   = 'rgba(124, 136, 153, 0.08)';
            badgeEl.style.borderColor  = 'rgba(124, 136, 153, 0.20)';
            badgeEl.style.color        = 'var(--text-secondary)';
            badgeEl.querySelector('.status-pill-dot').style.background = 'var(--text-secondary)';
            badgeLabelEl.textContent   = 'Host';
        }
    } else {
        badgeEl.style.display = 'none';
    }
}

/**
 * Start polling /api/sandbox/setup/status and update the logs console.
 */
function startSetupPolling() {
    const logsContainer = document.getElementById('setup-logs-container');
    const logsEl        = document.getElementById('setup-logs');
    logsContainer.style.display = 'block';

    if (setupPollingInterval) clearInterval(setupPollingInterval);

    setupPollingInterval = setInterval(async () => {
        try {
            const data = await getSandboxSetupStatus();

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

/**
 * Fetch sandbox status and update the sandbox screen UI.
 */
export async function loadSandbox() {
    try {
        const data = await getSandboxStatus();

        const isWindows = (data.os === 'windows');

        const statusLabelEl    = document.querySelector('#screen-sandbox .status-card:nth-child(1) .status-card-label');
        const statusDescEl     = document.querySelector('#screen-sandbox .status-card:nth-child(1) .desc');
        const provisionLabelEl = document.querySelector('#screen-sandbox .status-card:nth-child(2) .status-card-label');
        const provisionDescEl  = document.querySelector('#screen-sandbox .status-card:nth-child(2) .desc');
        const setupHeaderTitle = document.getElementById('setup-header-title');
        const setupPanelDesc   = document.getElementById('setup-panel-desc');
        const btnWslSetup      = document.getElementById('btn-wsl-setup');

        if (isWindows) {
            statusLabelEl.textContent    = 'Sandboxing Status';
            statusDescEl.textContent     = 'Is WSL filesystem containment currently active?';
            provisionLabelEl.textContent = 'WSL Provision';
            provisionDescEl.textContent  = 'Has the isolated WSL instance been provisioned?';
            setupHeaderTitle.textContent = 'WSL Setup Utilities';
            setupPanelDesc.textContent   = 'If WSL is not yet provisioned, trigger the automatic downloader and setup script here.';
            if (!btnWslSetup.disabled) {
                btnWslSetup.textContent  = 'Install & Provision WSL Sandbox';
            }
        } else {
            statusLabelEl.textContent    = 'Sandboxing Status';
            statusDescEl.textContent     = 'Is gVisor filesystem containment currently active?';
            provisionLabelEl.textContent = 'gVisor Setup';
            provisionDescEl.textContent  = 'Has the local gVisor (runsc) binary been configured?';
            setupHeaderTitle.textContent = 'gVisor Setup Utilities';
            setupPanelDesc.textContent   = 'If gVisor (runsc) is not yet configured, trigger the automatic downloader and setup script here.';
            if (!btnWslSetup.disabled) {
                btnWslSetup.textContent  = 'Install & Configure gVisor Sandbox';
            }
        }

        const statusEl       = document.getElementById('sb-status-text');
        statusEl.textContent = data.sandboxing_enabled ? 'Active' : 'Disabled';
        statusEl.className   = 'status-value ' + (data.sandboxing_enabled ? 'online' : 'offline');

        const wslEl       = document.getElementById('sb-wsl-text');
        wslEl.textContent = data.setup_done ? 'Provisioned' : 'Not Installed';
        wslEl.className   = 'status-value ' + (data.setup_done ? 'online' : 'offline');

        document.getElementById('sb-cache-text').textContent = data.cache_dir || 'N/A';

        const actionsRow = document.getElementById('sandbox-util-actions');
        actionsRow.style.display = data.setup_done ? 'flex' : 'none';

        updateContainmentBadge(data);
    } catch (err) {
        console.error('Failed to load sandbox status', err);
    }
}

/**
 * Wire up all sandbox button click handlers. Called once from main.js.
 */
export function initSandboxControls() {
    const btnWslSetup = document.getElementById('btn-wsl-setup');
    btnWslSetup.addEventListener('click', async () => {
        btnWslSetup.disabled    = true;
        btnWslSetup.textContent = 'Installing / Provisioning…';
        try {
            const data = await triggerSandboxSetup();
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
        btnSandboxTest.disabled    = true;
        btnSandboxTest.textContent = 'Testing Containment…';
        try {
            const data = await testSandbox();
            if (data.status === 'success') {
                showToast(`Test Succeeded!\nOutput: ${data.output}`, 'success');
            } else {
                showToast(data.message || 'Test failed.', 'error');
            }
        } catch (err) {
            showToast('Failed to execute sandbox containment test.', 'error');
        } finally {
            btnSandboxTest.disabled    = false;
            btnSandboxTest.textContent = 'Test Containment';
        }
    });

    const btnSandboxReset = document.getElementById('btn-sandbox-reset');
    btnSandboxReset.addEventListener('click', async () => {
        if (!confirm('Are you sure you want to completely wipe and reset the sandbox workspace? All modifications inside the sandbox will be lost.')) return;

        btnSandboxReset.disabled    = true;
        btnSandboxReset.textContent = 'Resetting Workspace…';
        try {
            const data = await resetSandbox();
            if (data.status === 'success') {
                showToast('Sandbox workspace reset successfully.', 'success');
            } else {
                showToast(data.message || 'Reset failed.', 'error');
            }
        } catch (err) {
            showToast('Failed to reset sandbox workspace.', 'error');
        } finally {
            btnSandboxReset.disabled    = false;
            btnSandboxReset.textContent = 'Reset Workspace';
            loadSandbox();
        }
    });
}
