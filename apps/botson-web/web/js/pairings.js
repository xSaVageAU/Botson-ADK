/**
 * pairings.js — Pending pairings screen: table rendering and approval.
 */

import { showToast } from './ui.js';
import { getPairings, approvePairing as apiApprovePairing } from './api.js';

/**
 * Fetch and render all pending pairing requests into the table.
 */
export async function loadPairings() {
    const tbody = document.getElementById('pairings-tbody');
    try {
        const data = await getPairings();

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
            const tr   = document.createElement('tr');
            const cols = [p.gateway, p.user_id, p.username, p.code];
            cols.forEach(text => {
                const td       = document.createElement('td');
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
        const data = await apiApprovePairing(gateway, code);
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
