const form = document.getElementById('input-form');
const input = document.getElementById('message-input');
const container = document.getElementById('chat-container');
const sendBtn = document.getElementById('send-btn');

form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text) return;

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
            body: JSON.stringify({ message: text })
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
        // Quick markdown code block formatter using backticks
        let formatted = text
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/`([^`\n]+)`/g, '<code>$1</code>');
        
        // Block code formatting split by triple backticks
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
