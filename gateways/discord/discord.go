package discord

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

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

		// Trigger typing indicator
		s.ChannelTyping(m.ChannelID)

		// Run query
		response, err := runFn(ctx, sessionKey, prompt)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error running query: %v", err))
			return
		}

		if response == "" {
			response = "(empty response)"
		}

		s.ChannelMessageSend(m.ChannelID, response)
	})

	// Add event handler for native slash commands
	dg.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
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
