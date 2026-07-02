package main

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	botsonAgent "botson/agent"
	"botson/internal/auth"
	"botson/internal/config"
	"botson/internal/executor"
	customplugin "botson/internal/plugin"
	"botson/internal/prompt"
	"botson/internal/tools"
	configtools "botson/internal/tools/config"
	timetools "botson/internal/tools/time"
	"botson/providers"
)

var (
	agentRunner *runner.Runner
	agentName   string
)

type ChatRequest struct {
	Message string `json:"message"`
}

type ChatResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Resolve default configuration and data paths
	cfgPath, dataDir, err := config.DefaultPaths()
	if err != nil {
		log.Fatalf("Failed to resolve configuration paths: %v", err)
	}

	// Initialize configuration manager
	mgr, err := config.NewManagerWithDataDir(cfgPath, dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}
	cfg := mgr.Get()

	// Set data directory for authorization and pairings
	auth.SetDataDir(dataDir)

	dbPath := filepath.Join(dataDir, "botson.db")
	sessSvc, err := database.NewSessionService(sqlite.Open(dbPath))
	if err != nil {
		log.Fatalf("Failed to initialize session service: %v", err)
	}
	if err := database.AutoMigrate(sessSvc); err != nil {
		log.Fatalf("Failed to migrate session database: %v", err)
	}

	// Force OpenRouter provider configurations for this specific web application
	modelGetter := func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.Model
		}
		return "openrouter/owl-alpha"
	}
	apiKeyGetter := func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}

	m, err := providers.GetModel(ctx, "openrouter", modelGetter, apiKeyGetter)
	if err != nil {
		log.Fatalf("Failed to initialize OpenRouter LLM provider: %v", err)
	}

	// Initialize config tools
	readTool, err := configtools.MakeReadConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create read_config tool: %v", err)
	}
	writeTool, err := configtools.MakeUpdateConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create update_config tool: %v", err)
	}
	timeTool, err := timetools.MakeGetTimeTool()
	if err != nil {
		log.Fatalf("Failed to create get_time tool: %v", err)
	}

	// Initialize Executor Manager
	execMgr := executor.NewManager(filepath.Join(dataDir, "cache"), "", cfg.Features.Sandboxing)
	defer execMgr.Close()

	execTools, err := tools.MakeAllTools(execMgr, cfg.Features)
	if err != nil {
		log.Fatalf("Failed to create executor tools: %v", err)
	}

	toolsList := []tool.Tool{readTool, writeTool, timeTool}
	toolsList = append(toolsList, execTools...)

	resolvedInstruction := prompt.ResolvePlaceholders(cfg.Instruction)
	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, resolvedInstruction, toolsList)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	agentName = ag.Name()

	schedulerPlugin, err := customplugin.NewSequentialToolPlugin()
	if err != nil {
		log.Fatalf("Failed to create scheduler plugin: %v", err)
	}

	agentRunner, err = runner.New(runner.Config{
		AppName:           ag.Name(),
		Agent:             ag,
		SessionService:    sessSvc,
		AutoCreateSession: true,
		PluginConfig: runner.PluginConfig{
			Plugins: []*adkplugin.Plugin{schedulerPlugin},
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize agent runner: %v", err)
	}

	// Start server routing
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/chat", handleChat)

	port := ":8080"
	log.Printf("Starting Botson Web UI on http://localhost%s using OpenRouter...\n", port)
	
	server := &http.Server{Addr: port}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Botson Web UI...")
	_ = server.Shutdown(context.Background())
	auth.CloseDB()
	log.Println("Web server stopped successfully.")
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.New("index").Parse(htmlIndex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmpl.Execute(w, nil)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	sessionID := "web:session"

	events := agentRunner.Run(r.Context(), agentName, sessionID, &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: req.Message},
		},
	}, agent.RunConfig{
		StreamingMode: agent.StreamingModeNone,
	})

	var result strings.Builder
	var errText string
	for ev, err := range events {
		if err != nil {
			errText = err.Error()
			break
		}
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					result.WriteString(part.Text)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	resp := ChatResponse{
		Response: result.String(),
		Error:    errText,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

const htmlIndex = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Botson Web UI (OpenRouter)</title>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;600&family=Space+Mono&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-primary: #0b0f19;
            --bg-secondary: #131a2e;
            --accent: #6366f1;
            --accent-glow: rgba(99, 102, 241, 0.15);
            --text-primary: #f8fafc;
            --text-secondary: #94a3b8;
            --border: #1e293b;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            font-family: 'Outfit', sans-serif;
            background-color: var(--bg-primary);
            color: var(--text-primary);
            display: flex;
            flex-direction: column;
            height: 100vh;
            overflow: hidden;
        }

        header {
            background-color: var(--bg-secondary);
            border-bottom: 1px solid var(--border);
            padding: 1.2rem 2rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        header h1 {
            font-size: 1.4rem;
            font-weight: 600;
            letter-spacing: -0.025em;
            background: linear-gradient(135deg, #a5b4fc, var(--accent));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .badge {
            background-color: var(--accent-glow);
            border: 1px solid var(--accent);
            color: #818cf8;
            padding: 0.25rem 0.75rem;
            border-radius: 9999px;
            font-size: 0.8rem;
            font-weight: 600;
        }

        #chat-container {
            flex: 1;
            padding: 2rem;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
            max-width: 1000px;
            width: 100%;
            margin: 0 auto;
        }

        .message {
            max-width: 80%;
            padding: 1rem 1.25rem;
            border-radius: 1.25rem;
            line-height: 1.5;
            font-size: 0.95rem;
            animation: fadeIn 0.3s cubic-bezier(0.16, 1, 0.3, 1) forwards;
            word-wrap: break-word;
        }

        .message.user {
            background-color: var(--accent);
            color: #ffffff;
            align-self: flex-end;
            border-bottom-right-radius: 0.25rem;
            box-shadow: 0 4px 12px var(--accent-glow);
        }

        .message.agent {
            background-color: var(--bg-secondary);
            border: 1px solid var(--border);
            color: var(--text-primary);
            align-self: flex-start;
            border-bottom-left-radius: 0.25rem;
        }

        .message pre {
            background-color: #080c14;
            padding: 1rem;
            border-radius: 0.5rem;
            overflow-x: auto;
            margin: 0.75rem 0;
            font-family: 'Space Mono', monospace;
            font-size: 0.85rem;
            border: 1px solid #1e293b;
        }

        .message code {
            font-family: 'Space Mono', monospace;
            background-color: #080c14;
            padding: 0.15rem 0.3rem;
            border-radius: 0.25rem;
            font-size: 0.85rem;
        }

        #input-container {
            background-color: var(--bg-secondary);
            border-top: 1px solid var(--border);
            padding: 1.5rem 2rem;
        }

        #input-form {
            max-width: 1000px;
            margin: 0 auto;
            display: flex;
            gap: 1rem;
            background-color: var(--bg-primary);
            padding: 0.5rem;
            border-radius: 9999px;
            border: 1px solid var(--border);
            transition: border-color 0.2s, box-shadow 0.2s;
        }

        #input-form:focus-within {
            border-color: var(--accent);
            box-shadow: 0 0 0 4px var(--accent-glow);
        }

        #message-input {
            flex: 1;
            background: none;
            border: none;
            outline: none;
            color: var(--text-primary);
            padding: 0.75rem 1.5rem;
            font-family: inherit;
            font-size: 1rem;
        }

        #message-input::placeholder {
            color: var(--text-secondary);
        }

        #send-btn {
            background-color: var(--accent);
            color: #ffffff;
            border: none;
            padding: 0.75rem 1.75rem;
            border-radius: 9999px;
            font-weight: 600;
            cursor: pointer;
            transition: opacity 0.2s;
        }

        #send-btn:hover {
            opacity: 0.9;
        }

        #send-btn:disabled {
            background-color: var(--border);
            color: var(--text-secondary);
            cursor: not-allowed;
        }

        .typing-indicator {
            align-self: flex-start;
            background-color: var(--bg-secondary);
            border: 1px solid var(--border);
            padding: 0.75rem 1.25rem;
            border-radius: 1.25rem;
            border-bottom-left-radius: 0.25rem;
            display: flex;
            align-items: center;
            gap: 0.35rem;
        }

        .typing-indicator span {
            width: 6px;
            height: 6px;
            background-color: var(--text-secondary);
            border-radius: 50%;
            animation: bounce 1.4s infinite ease-in-out both;
        }

        .typing-indicator span:nth-child(1) { animation-delay: -0.32s; }
        .typing-indicator span:nth-child(2) { animation-delay: -0.16s; }

        @keyframes fadeIn {
            from { opacity: 0; transform: translateY(8px); }
            to { opacity: 1; transform: translateY(0); }
        }

        @keyframes bounce {
            0%, 80%, 100% { transform: scale(0); }
            40% { transform: scale(1); }
        }
    </style>
</head>
<body>
    <header>
        <h1>Botson</h1>
        <span class="badge">OpenRouter Mode</span>
    </header>

    <div id="chat-container">
        <div class="message agent">
            Hello! I am Botson, configured here specifically using OpenRouter. How can I assist you with your coding workspace tasks today?
        </div>
    </div>

    <div id="input-container">
        <form id="input-form">
            <input type="text" id="message-input" placeholder="Type a message or command..." required autocomplete="off">
            <button type="submit" id="send-btn">Send</button>
        </form>
    </div>

    <script>
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
                // Quick markdown code block formatter using ASCII hex codes to avoid Go backtick issues
                let formatted = text
                    .replace(/&/g, "&amp;")
                    .replace(/</g, "&lt;")
                    .replace(/>/g, "&gt;")
                    .replace(/\x60([^\x60\n]+)\x60/g, '<code>$1</code>');
                
                // Block code formatting split by hex representation of triple backticks
                const codeBlocks = formatted.split('\x60\x60\x60');
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
    </script>
</body>
</html>
`
