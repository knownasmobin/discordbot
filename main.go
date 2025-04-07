package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"discordbot/audio"
	"discordbot/audio/spotify"
	"discordbot/audio/youtube"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

var (
	voiceManager  *audio.VoiceManager
	youtubeClient *youtube.Client
	spotifyClient *spotify.Client
	commands      = []*discordgo.ApplicationCommand{
		{
			Name:        "ping",
			Description: "Responds with Pong!",
		},
		{
			Name:        "join",
			Description: "Join your voice channel",
		},
		{
			Name:        "leave",
			Description: "Leave the voice channel",
		},
		{
			Name:        "play",
			Description: "Play a YouTube or Spotify URL",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "url",
					Description: "The URL to play",
					Required:    true,
				},
			},
		},
		{
			Name:        "queue",
			Description: "View or add to the queue",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "url",
					Description: "The URL to add to the queue",
					Required:    false,
				},
			},
		},
		{
			Name:        "repeat",
			Description: "Toggle repeat mode",
		},
		{
			Name:        "autoplay",
			Description: "Toggle autoplay mode",
		},
	}
)

func init() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Initialize voice manager
	voiceManager = audio.NewVoiceManager()

	// Initialize YouTube client
	youtubeClient = youtube.NewClient()

	// Initialize Spotify client with YouTube search function
	spotifyClient, err = spotify.NewClient(youtube.Search)
	if err != nil {
		log.Printf("Warning: Spotify client initialization failed: %v", err)
		log.Printf("Spotify functionality will be disabled")
	}
}

func main() {
	// Create a new Discord session using the token from .env
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN not found in .env file")
	}

	discord, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Error creating Discord session: ", err)
	}

	// Register the interaction handler
	discord.AddHandler(interactionCreate)

	// Open a websocket connection to Discord and begin listening
	err = discord.Open()
	if err != nil {
		log.Fatal("Error opening connection: ", err)
	}

	// Register commands
	log.Println("Registering commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, command := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, "", command)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", command.Name, err)
		}
		registeredCommands[i] = cmd
	}

	fmt.Println("Bot is now running. Press Ctrl+C to exit.")

	// Wait here until CTRL+C or other term signal is received
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Clean up
	for _, instance := range voiceManager.Instances {
		if instance.Connection != nil {
			instance.Leave()
		}
	}

	// Delete all commands when shutting down
	log.Println("Removing commands...")
	for _, cmd := range registeredCommands {
		err := discord.ApplicationCommandDelete(discord.State.User.ID, "", cmd.ID)
		if err != nil {
			log.Panicf("Cannot delete '%v' command: %v", cmd.Name, err)
		}
	}

	// Cleanly close down the Discord session
	discord.Close()
}

func interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Handle the command
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	// Get the voice instance for this guild
	vi := voiceManager.GetVoiceInstance(i.GuildID)

	switch i.ApplicationCommandData().Name {
	case "ping":
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Pong!",
			},
		})

	case "join":
		// Find the user's voice channel
		vs, err := findUserVoiceState(s, i.GuildID, i.Member.User.ID)
		if err != nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You need to be in a voice channel first!",
				},
			})
			return
		}

		// Respond to the interaction before joining
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Joining your voice channel...",
			},
		})

		// Join the voice channel
		err = vi.Join(s, vs.ChannelID)
		if err != nil {
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: fmt.Sprintf("Error joining voice channel: %v", err),
			})
			return
		}

		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Joined voice channel!",
		})

	case "leave":
		if vi.Connection == nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "I'm not in a voice channel!",
				},
			})
			return
		}

		// Respond to the interaction
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Leaving voice channel...",
			},
		})

		// Leave the voice channel
		err := vi.Leave()
		if err != nil {
			s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Content: fmt.Sprintf("Error leaving voice channel: %v", err),
			})
			return
		}

		s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Left voice channel!",
		})

	case "play":
		// Get the URL option
		options := i.ApplicationCommandData().Options
		url := options[0].StringValue()

		// Check if we're in a voice channel
		if vi.Connection == nil {
			// Try to join the user's voice channel
			vs, err := findUserVoiceState(s, i.GuildID, i.Member.User.ID)
			if err != nil {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "You need to be in a voice channel first!",
					},
				})
				return
			}

			// Respond to the interaction first
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("Adding to queue: %s\nJoining your voice channel...", url),
				},
			})

			// Join the user's voice channel
			err = vi.Join(s, vs.ChannelID)
			if err != nil {
				s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
					Content: fmt.Sprintf("Error joining voice channel: %v", err),
				})
				return
			}
		} else {
			// Respond to the interaction
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("Adding to queue: %s", url),
				},
			})
		}

		// Add the URL to the queue
		vi.AddToQueue(url)

		// If nothing is playing, start playing
		if !vi.IsPlaying {
			go playNextInQueue(s, i.ChannelID, vi)
		}

	case "queue":
		options := i.ApplicationCommandData().Options

		if len(options) == 0 {
			// Show the current queue
			vi.Mu.Lock()
			if len(vi.Queue) == 0 {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "The queue is empty",
					},
				})
			} else {
				queueMsg := "Current queue:\n"
				for idx, url := range vi.Queue {
					queueMsg += fmt.Sprintf("%d. %s\n", idx+1, url)
				}
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: queueMsg,
					},
				})
			}
			vi.Mu.Unlock()
		} else {
			// Add URL to queue
			url := options[0].StringValue()

			// Check if we're in a voice channel
			if vi.Connection == nil {
				// Try to join the user's voice channel
				vs, err := findUserVoiceState(s, i.GuildID, i.Member.User.ID)
				if err != nil {
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData{
							Content: "You need to be in a voice channel first!",
						},
					})
					return
				}

				// Respond to the interaction first
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("Adding to queue: %s\nJoining your voice channel...", url),
					},
				})

				// Join the user's voice channel
				err = vi.Join(s, vs.ChannelID)
				if err != nil {
					s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
						Content: fmt.Sprintf("Error joining voice channel: %v", err),
					})
					return
				}
			} else {
				// Respond to the interaction
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("Adding to queue: %s", url),
					},
				})
			}

			// Add the URL to the queue
			vi.AddToQueue(url)

			// If nothing is playing, start playing
			if !vi.IsPlaying {
				go playNextInQueue(s, i.ChannelID, vi)
			}
		}

	case "repeat":
		// Toggle repeat mode
		vi.Mu.Lock()
		vi.Repeat = !vi.Repeat
		status := "enabled"
		if !vi.Repeat {
			status = "disabled"
		}
		vi.Mu.Unlock()

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Repeat mode %s", status),
			},
		})

	case "autoplay":
		// Toggle autoplay mode
		vi.Mu.Lock()
		vi.Autoplay = !vi.Autoplay
		status := "enabled"
		if !vi.Autoplay {
			status = "disabled"
		}
		vi.Mu.Unlock()

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Autoplay mode %s", status),
			},
		})
	}
}

// findUserVoiceState finds a user's voice state in a guild
func findUserVoiceState(s *discordgo.Session, guildID, userID string) (*discordgo.VoiceState, error) {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return nil, err
	}

	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return vs, nil
		}
	}

	return nil, fmt.Errorf("user not found in voice channels")
}

// playNextInQueue plays the next item in the queue
func playNextInQueue(s *discordgo.Session, channelID string, vi *audio.VoiceInstance) {
	url, ok := vi.GetNextFromQueue()
	if !ok {
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
		return
	}

	vi.Mu.Lock()
	vi.IsPlaying = true
	vi.Mu.Unlock()

	// Send message to channel
	message, _ := s.ChannelMessageSend(channelID, fmt.Sprintf("Playing: %s", url))

	var err error
	// Determine if it's a YouTube or Spotify URL
	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
		err = youtubeClient.Play(vi.Connection, url)
	} else if strings.Contains(url, "spotify.com") {
		if spotifyClient == nil {
			s.ChannelMessageSend(channelID, "Spotify support is not available")
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
			return
		}

		err = spotifyClient.PlayTrack(vi.Connection, url)
	} else {
		s.ChannelMessageSend(channelID, "Unsupported URL. Please provide a YouTube or Spotify URL.")
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
		return
	}

	if err != nil {
		// Check for age restriction error
		if strings.Contains(err.Error(), "login required to confirm your age") {
			s.ChannelMessageSend(channelID, "⚠️ **Cannot play this video: Age-restricted content**\nThis video requires age verification on YouTube.")
		} else {
			s.ChannelMessageSend(channelID, fmt.Sprintf("Error playing: %v", err))
		}
	}

	// Edit message to indicate track finished playing
	s.ChannelMessageEdit(channelID, message.ID, fmt.Sprintf("Finished playing: %s", url))

	vi.Mu.Lock()
	// Check repeat mode
	if vi.Repeat {
		// Add the current URL back to the queue
		vi.Queue = append(vi.Queue, vi.CurrentURL)
	}

	// Check if autoplay should continue with next song
	continuePlay := len(vi.Queue) > 0 || vi.Autoplay
	vi.Mu.Unlock()

	if continuePlay {
		// If we're in autoplay mode and the queue is empty, try to find a related video
		vi.Mu.Lock()
		isQueueEmpty := len(vi.Queue) == 0
		isAutoplay := vi.Autoplay
		currentURL := vi.CurrentURL
		vi.Mu.Unlock()

		if isQueueEmpty && isAutoplay {
			// Find a related video based on the current URL
			var relatedURL string
			var err error

			if strings.Contains(currentURL, "youtube.com") || strings.Contains(currentURL, "youtu.be") {
				// Get related YouTube video
				relatedURL, err = youtubeClient.GetRelatedVideo(currentURL)
				if err != nil {
					s.ChannelMessageSend(channelID, fmt.Sprintf("Failed to find related video: %v", err))
					// Fallback to repeating the current track
					relatedURL = currentURL
				}
			} else if strings.Contains(currentURL, "spotify.com") {
				// Get related Spotify track
				if spotifyClient != nil {
					relatedURL, err = spotifyClient.GetRelatedTrack(currentURL)
					if err != nil {
						s.ChannelMessageSend(channelID, fmt.Sprintf("Failed to find related track: %v", err))
						// Fallback to repeating the current track
						relatedURL = currentURL
					}
				} else {
					// Spotify client unavailable, fallback to current URL
					relatedURL = currentURL
				}
			} else {
				// Unknown URL type, fallback to current URL
				relatedURL = currentURL
			}

			vi.AddToQueue(relatedURL)
			s.ChannelMessageSend(channelID, fmt.Sprintf("Autoplay: Adding related track to queue: %s", relatedURL))
		}

		// Play the next item in the queue
		go playNextInQueue(s, channelID, vi)
	} else {
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
	}
}
