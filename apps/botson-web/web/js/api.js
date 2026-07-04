/**
 * api.js — Thin fetch wrappers for all Botson HTTP endpoints.
 * All functions return parsed JSON or throw on network/HTTP error.
 */

export async function getSessions() {
    const r = await fetch('/api/sessions');
    return r.json();
}

export async function getModelInfo() {
    const r = await fetch('/api/model-info');
    return r.json();
}

export async function createSession() {
    const r = await fetch('/api/sessions/create', { method: 'POST' });
    return r.json();
}

/**
 * @param {string} id
 * @param {string} userId
 */
export async function deleteSession(id, userId) {
    return fetch(`/api/sessions/delete?id=${id}&user_id=${userId || 'user'}`, { method: 'DELETE' });
}

/**
 * @param {string} id
 * @param {string} userId
 */
export async function getSession(id, userId) {
    const r = await fetch(`/api/sessions/get?id=${id}&user_id=${userId || 'user'}`);
    return r.json();
}

export async function getSandboxStatus() {
    const r = await fetch('/api/sandbox');
    return r.json();
}

export async function triggerSandboxSetup() {
    const r = await fetch('/api/sandbox/setup', { method: 'POST' });
    return r.json();
}

export async function getSandboxSetupStatus() {
    const r = await fetch('/api/sandbox/setup/status');
    return r.json();
}

export async function resetSandbox() {
    const r = await fetch('/api/sandbox/reset', { method: 'POST' });
    return r.json();
}

export async function testSandbox() {
    const r = await fetch('/api/sandbox/test', { method: 'POST' });
    return r.json();
}

export async function getConfig() {
    const r = await fetch('/api/config');
    return r.json();
}

/**
 * @param {object} payload
 */
export async function saveConfig(payload) {
    const r = await fetch('/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
    });
    return r.json();
}

export async function getPairings() {
    const r = await fetch('/api/pairings');
    return r.json();
}

/**
 * @param {string} gateway
 * @param {string} code
 */
export async function approvePairing(gateway, code) {
    const r = await fetch('/api/pairings/approve', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gateway, code }),
    });
    return r.json();
}

/**
 * POST /api/chat and return a ReadableStream reader for SSE processing.
 * @param {string} message
 * @param {string} sessionId
 * @param {string} userId
 * @returns {ReadableStreamDefaultReader}
 */
export async function sendChatMessage(message, sessionId, userId) {
    const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, session_id: sessionId, user_id: userId }),
    });
    return response.body.getReader();
}
