package discord

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"botson/internal/auth"
	"botson/internal/commands"
)

type DiscordGateway struct {
	token   string
	session *discordgo.Session
}

func NewDiscordGateway(token string) *DiscordGateway {
	return &DiscordGateway{
		token: token,
	}
}

func (dg *DiscordGateway) Name() string {
	return "Discord"
}

func (dg *DiscordGateway) Start(ctx context.Context, runFn func(ctx context.Context, sessionID string, query string) (string, error)) error {
	if dg.token == "" || dg.token == "YOUR_DISCORD_TOKEN" {
		log.Println("[Discord] Discord token is not configured. Gateway will not start.")
		return nil
	}

	session, err := discordgo.New("Bot " + dg.token)
	if err != nil {
		return fmt.Errorf("failed to create discord session: %w", err)
	}
	dg.session = session

	// Configure Intents
	dg.session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent

	// Add event handler for message creation (chatting)
	dg.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore messages created by the bot itself or other bots
		if m.Author.ID == s.State.User.ID || m.Author.Bot {
			return
		}

		isDM := false
		channel, err := s.State.Channel(m.ChannelID)
		if err == nil {
			isDM = (channel.Type == discordgo.ChannelTypeDM)
		} else {
			// Fallback channel lookup
			c, err := s.Channel(m.ChannelID)
			if err == nil {
				isDM = (c.Type == discordgo.ChannelTypeDM)
			}
		}

		isMentioned := false
		for _, mention := range m.Mentions {
			if mention.ID == s.State.User.ID {
				isMentioned = true
				break
			}
		}

		// Only respond to DMs or Mentions
		if !isDM && !isMentioned {
			return
		}

		if isDM {
			authed, code, err := auth.CheckAuth("discord", m.Author.ID, m.Author.Username)
			if err != nil {
				sendErrorEmbed(s, m.ChannelID, fmt.Sprintf("Error checking authentication: %v", err))
				return
			}
			if !authed {
				sendAuthEmbed(s, m.ChannelID, code)
				return
			}
		}

		prompt := m.Content
		if isMentioned {
			// Strip mentions out of prompt text
			mentionStr := fmt.Sprintf("<@%s>", s.State.User.ID)
			prompt = strings.ReplaceAll(prompt, mentionStr, "")
			mentionNickStr := fmt.Sprintf("<@!%s>", s.State.User.ID)
			prompt = strings.ReplaceAll(prompt, mentionNickStr, "")
			prompt = strings.TrimSpace(prompt)
		}

		// Use platform-prefixed channel key to identify the session
		sessionKey := "discord:" + m.ChannelID

		// Add loading reaction and start typing loop
		addReaction(s, m.ChannelID, m.ID, "⏳")
		stopTyping := startTypingLoop(s, m.ChannelID)

		// Run query
		response, err := runFn(ctx, sessionKey, prompt)
		stopTyping()

		if err != nil {
			removeReaction(s, m.ChannelID, m.ID, "⏳")
			addReaction(s, m.ChannelID, m.ID, "❌")
			sendErrorEmbed(s, m.ChannelID, err.Error())
			return
		}

		removeReaction(s, m.ChannelID, m.ID, "⏳")
		addReaction(s, m.ChannelID, m.ID, "✅")

		if response == "" {
			response = "(empty response)"
		}

		// Split response to fit Discord's 2,000-character message limit
		chunks := splitMessage(response, 2000)
		for _, chunk := range chunks {
			_, _ = s.ChannelMessageSend(m.ChannelID, chunk)
		}
	})

	// Add event handler for native slash commands
	dg.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}

		if i.User != nil { // DM
			authed, code, err := auth.CheckAuth("discord", i.User.ID, i.User.Username)
			if err != nil {
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("❌ Error checking auth: %v", err),
					},
				})
				return
			}
			if !authed {
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("Hello! I don't recognize you yet. To enable chatting in this session, please provide your pairing code: `%s` to the bot owner and ask them to run:\n`botson pairing approve discord %s`", code, code),
					},
				})
				return
			}
		}

		data := i.ApplicationCommandData()
		sessionKey := "discord:" + i.ChannelID
		cmdCtx := commands.CommandContext{
			SessionKey: sessionKey,
		}

		// Execute the universal command
		resp, err := commands.Execute(ctx, data.Name, cmdCtx, "")
		if err != nil {
			errResp := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ Error executing command: %v", err),
				},
			})
			if errResp != nil {
				log.Printf("[Discord] Error sending error interaction response: %v", errResp)
			}
			return
		}

		errResp := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: resp,
			},
		})
		if errResp != nil {
			log.Printf("[Discord] Error sending interaction response: %v", errResp)
		}
	})

	if err := dg.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord gateway session: %w", err)
	}

	log.Println("[Discord] Discord gateway is active and connected.")

	// Register native slash commands with Discord API
	appID := dg.session.State.User.ID
	log.Println("[Discord] Registering native application commands...")
	for _, cmd := range commands.GetCommands() {
		appCmd := &discordgo.ApplicationCommand{
			Name:        cmd.Name,
			Description: cmd.Description,
		}
		_, err := dg.session.ApplicationCommandCreate(appID, "", appCmd)
		if err != nil {
			log.Printf("[Discord] Failed to register slash command %q: %v", cmd.Name, err)
		} else {
			log.Printf("[Discord] Registered native slash command: /%s", cmd.Name)
		}
	}

	<-ctx.Done()
	return nil
}

func (dg *DiscordGateway) Stop() error {
	if dg.session != nil {
		log.Println("[Discord] Closing Discord session...")
		return dg.session.Close()
	}
	return nil
}

// startTypingLoop starts a goroutine to continuously trigger the typing indicator.
// Returns a function to cancel the typing loop.
func startTypingLoop(s *discordgo.Session, channelID string) func() {
	stop := make(chan struct{})
	// Trigger typing immediately
	_ = s.ChannelTyping(channelID)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = s.ChannelTyping(channelID)
			case <-stop:
				return
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
		})
	}
}

// splitMessage splits long text responses into chunks of <= 2000 characters.
func splitMessage(content string, limit int) []string {
	if len(content) <= limit {
		return []string{content}
	}

	var chunks []string
	runes := []rune(content)
	for len(runes) > 0 {
		if len(runes) <= limit {
			chunks = append(chunks, string(runes))
			break
		}

		splitIdx := limit

		// Try to split at a double-newline paragraph boundary
		lastParagraph := strings.LastIndex(string(runes[:limit]), "\n\n")
		if lastParagraph > limit/2 {
			splitIdx = lastParagraph + 2
		} else {
			// Try to split at a single newline
			lastNewline := strings.LastIndex(string(runes[:limit]), "\n")
			if lastNewline > limit/2 {
				splitIdx = lastNewline + 1
			} else {
				// Try to split at a space
				lastSpace := strings.LastIndex(string(runes[:limit]), " ")
				if lastSpace > limit/2 {
					splitIdx = lastSpace + 1
				}
			}
		}

		chunks = append(chunks, string(runes[:splitIdx]))
		runes = runes[splitIdx:]
	}

	return chunks
}

// addReaction adds an emoji reaction to a user message.
func addReaction(s *discordgo.Session, channelID, messageID, emoji string) {
	_ = s.MessageReactionAdd(channelID, messageID, emoji)
}

// removeReaction removes an emoji reaction added by the bot.
func removeReaction(s *discordgo.Session, channelID, messageID, emoji string) {
	_ = s.MessageReactionRemove(channelID, messageID, emoji, "@me")
}

// sendAuthEmbed sends a beautiful rich Embed for pairing code verification.
func sendAuthEmbed(s *discordgo.Session, channelID, pairingCode string) {
	embed := &discordgo.MessageEmbed{
		Title:       "🔒 Authentication Required",
		Description: "I don't recognize your account yet. To enable direct chatting, please ask the bot owner to approve your pairing request.",
		Color:       0x3498db, // Premium blue
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Pairing Code",
				Value:  fmt.Sprintf("`%s`", pairingCode),
				Inline: true,
			},
			{
				Name:   "Approval Command",
				Value:  fmt.Sprintf("`botson pairing approve discord %s`", pairingCode),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Run the command on the host machine running Botson.",
		},
	}
	_, _ = s.ChannelMessageSendEmbed(channelID, embed)
}

// sendErrorEmbed sends a stylized rich Embed for LLM or command errors.
func sendErrorEmbed(s *discordgo.Session, channelID, errText string) {
	embed := &discordgo.MessageEmbed{
		Title:       "❌ Error",
		Description: fmt.Sprintf("An error occurred while executing your request:\n```plain\n%s\n```", errText),
		Color:       0xe74c3c, // Premium red
	}
	_, _ = s.ChannelMessageSendEmbed(channelID, embed)
}
