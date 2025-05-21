package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

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

	// Initialize YouTube client with cache directory
	cacheDir := filepath.Join(os.TempDir(), "discordbot", "cache")
	youtubeClient = youtube.NewClient(cacheDir)

	// Initialize Spotify client (will be disabled if not configured)
	var spotifyErr error
	spotifyClient, spotifyErr = spotify.NewClient(youtubeClient)
	if spotifyErr != nil {
		log.Printf("Warning: Spotify client initialization failed: %v", spotifyErr)
		log.Printf("Spotify functionality will be disabled")
	}
}

// Global context for cancellation
var (
	ctx        context.Context
	cancelFunc context.CancelFunc
)

// cleanupChildProcesses ensures all child processes are terminated when the application exits
func cleanupChildProcesses() {
	log.Println("Cleaning up child processes...")
	
	// Try to get the process group ID
	pgid, err := syscall.Getpgid(0)
	if err != nil {
		log.Printf("Failed to get process group ID: %v", err)
		pgid = 0
	}
	
	// First try to kill the entire process group
	if pgid != 0 {
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			log.Printf("Failed to kill process group: %v", err)
		}
	}
	
	// Then try to kill individual child processes
	cmd := exec.Command("ps", "-o", "pid=", "--ppid", fmt.Sprint(os.Getpid()))
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to list child processes: %v", err)
		return
	}
	
	// Kill each child process
	for _, pidStr := range strings.Fields(string(output)) {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			log.Printf("Invalid PID %s: %v", pidStr, err)
			continue
		}
		
		// Try to kill the process
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			log.Printf("Failed to kill process %d: %v", pid, err)
		}
	}
}

func main() {
	// Ensure we clean up child processes on exit
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic: %v", r)
		}
		cleanupChildProcesses()
	}()

	// Create a context that will be canceled on interrupt
	ctx, cancelFunc = context.WithCancel(context.Background())
	defer func() {
		cancelFunc()
		cleanupChildProcesses()
	}()

	// Create a new Discord session using the token from .env
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN not found in .env file")
	}

	discord, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatal("Error creating Discord session: ", err)
	}

	// Store the Discord session in a way that's accessible to cleanup
	discord.StateEnabled = true
	discord.LogLevel = discordgo.LogDebug

	// Register the interaction handler
	discord.AddHandler(interactionCreate)

	// We need to define our intents
	discord.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	// Open a websocket connection to Discord and begin listening
	err = discord.Open()
	if err != nil {
		log.Fatal("Error opening connection: ", err)
	}

	// Register commands with global scope
	log.Println("Registering commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, command := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, "", command)
		if err != nil {
			log.Printf("Cannot create '%v' command: %v", command.Name, err)
			continue
		}
		registeredCommands[i] = cmd
		log.Printf("Registered command: %s", cmd.Name)
	}

	// Set up signal handling
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	// Start a goroutine to handle shutdown
	go func() {
		// Wait for a signal or context cancellation
		var sig os.Signal
		select {
		case sig = <-signalChan:
			log.Printf("Received signal: %v. Shutting down gracefully...\n", sig)
		case <-ctx.Done():
			sig = syscall.SIGTERM
			log.Println("Shutting down due to context cancellation...")
		}

		// Create a new context with timeout for graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Channel to track completion of cleanup
		cleanupDone := make(chan struct{})

		// Perform cleanup in a separate goroutine
		go func() {
			// Clean up voice connections
			log.Println("Disconnecting from voice channels...")
			for guildID, instance := range voiceManager.Instances {
				if instance != nil && instance.Connection != nil {
					log.Printf("Leaving voice channel in guild %s", guildID)
					if err := instance.Leave(); err != nil {
						log.Printf("Error leaving voice channel in guild %s: %v", guildID, err)
					}
				}
			}

			// Delete all registered commands
			log.Println("Removing application commands...")
			for i, cmd := range registeredCommands {
				if cmd != nil {
					log.Printf("Deleting command: %s", cmd.Name)
					if err := discord.ApplicationCommandDelete(discord.State.User.ID, "", cmd.ID); err != nil {
						log.Printf("Cannot delete '%v' command: %v", cmd.Name, err)
					} else {
						registeredCommands[i] = nil // Mark as deleted
					}
				}
			}

			// Close the Discord session
			log.Println("Closing Discord session...")
			if err := discord.Close(); err != nil {
				log.Printf("Error closing Discord session: %v", err)
			}

			close(cleanupDone)
		}()

		// Wait for cleanup to complete or timeout
		select {
		case <-cleanupDone:
			log.Println("Cleanup completed successfully")
		case <-shutdownCtx.Done():
			log.Printf("Cleanup timed out: %v", shutdownCtx.Err())
		}

		// Force exit if we received a second signal
		if sig == syscall.SIGINT || sig == syscall.SIGTERM {
			log.Println("Forcefully terminating...")
			// Use os.Exit(0) to ensure immediate termination
			os.Exit(0)
		} else {
			// For other signals, let the program exit normally
			log.Println("Shutdown complete")
		}
	}()

	log.Println("Bot is now running. Press Ctrl+C to exit.")

	// Block until context is done
	<-ctx.Done()
}

func interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Log all incoming interactions for debugging
	log.Printf("Received interaction: Type=%s, Command=%s, GuildID=%s, ChannelID=%s, UserID=%s", 
		i.Type.String(), 
		i.ApplicationCommandData().Name, 
		i.GuildID, 
		i.ChannelID, 
		i.Member.User.ID)

	// Handle the command
	if i.Type != discordgo.InteractionApplicationCommand {
		log.Printf("Ignoring non-command interaction: %s", i.Type.String())
		return
	}

	// Add a defer response to prevent "Unknown Integration" errors
	initialContent := "Processing your command..."
	log.Printf("Sending initial response for command: %s", i.ApplicationCommandData().Name)
	
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: initialContent,
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
		// Try to send a follow-up message if the initial response fails
		_, followUpErr := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
			Content: "Error: Failed to process your command. Please try again.",
		})
		if followUpErr != nil {
			log.Printf("Failed to send follow-up message: %v", followUpErr)
		}
		return
	}

	// Get the voice instance for this guild
	log.Printf("Getting voice instance for guild: %s", i.GuildID)
	vi := voiceManager.GetVoiceInstance(i.GuildID)
	log.Printf("Current voice instance state - IsPlaying: %v, Queue length: %d", vi.IsPlaying, len(vi.Queue))

	switch i.ApplicationCommandData().Name {
	case "ping":
		content := "Pong!"
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})

	case "join":
		// Find the user's voice channel
		vs, err := findUserVoiceState(s, i.GuildID, i.Member.User.ID)
		if err != nil {
			content := "You need to be in a voice channel first!"
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		// Join the voice channel
		err = vi.Join(s, vs.ChannelID)
		if err != nil {
			content := fmt.Sprintf("Error joining voice channel: %v", err)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		content := "Joined voice channel!"
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})

	case "leave":
		if vi.Connection == nil {
			content := "I'm not in a voice channel!"
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		// Leave the voice channel
		err := vi.Leave()
		if err != nil {
			content := fmt.Sprintf("Error leaving voice channel: %v", err)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		content := "Left voice channel!"
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})

	case "play":
		// Get the URL option
		options := i.ApplicationCommandData().Options
		url := options[0].StringValue()

		// Check if we're in a voice channel
		vs, err := findUserVoiceState(s, i.GuildID, i.Member.User.ID)
		if err != nil {
			content := "You need to be in a voice channel first!"
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		// Join or move to the user's voice channel
		err = vi.Join(s, vs.ChannelID)
		if err != nil {
			content := fmt.Sprintf("Error joining voice channel: %v", err)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		// Small delay to ensure voice connection is ready
		time.Sleep(500 * time.Millisecond)

		// Ensure we're connected to voice
		if vi.Connection == nil || vi.Connection.Ready != true {
			content := "Failed to connect to voice channel. Please try again."
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			return
		}

		log.Printf("Adding URL to queue: %s", url)
		vi.AddToQueue(url)
		log.Printf("Queue length after add: %d", len(vi.Queue))

		// Update the interaction to show we're starting to play
		content := fmt.Sprintf("Added to queue: %s", url)
		log.Printf("Updating interaction with queue status")
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})
		if err != nil {
			log.Printf("Failed to update interaction: %v", err)
		}

		vi.Mu.Lock()
		isPlaying := vi.IsPlaying
		vi.Mu.Unlock()

		log.Printf("Current play status - IsPlaying: %v", isPlaying)
		if !isPlaying {
			log.Printf("Starting playback in a new goroutine")
			go playNextInQueue(s, i.ChannelID, vi)
		} else {
			log.Printf("Already playing, added to queue")
		}

	case "queue":
		options := i.ApplicationCommandData().Options

		if len(options) == 0 {
			// Show the current queue
			vi.Mu.Lock()
			if len(vi.Queue) == 0 {
				content := "The queue is empty"
				s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: &content,
				})
			} else {
				queueMsg := "Current queue:\n"
				for idx, url := range vi.Queue {
					queueMsg += fmt.Sprintf("%d. %s\n", idx+1, url)
				}
				s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: &queueMsg,
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
					content := "You need to be in a voice channel first!"
					s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
						Content: &content,
					})
					return
				}

				// Join the user's voice channel
				err = vi.Join(s, vs.ChannelID)
				if err != nil {
					content := fmt.Sprintf("Error joining voice channel: %v", err)
					s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
						Content: &content,
					})
					return
				}
			}

			// Add the URL to the queue
			vi.AddToQueue(url)

			content := fmt.Sprintf("Added to queue: %s", url)
			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &content,
			})
			if err != nil {
				log.Printf("Failed to update interaction: %v", err)
			}

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

		content := fmt.Sprintf("Repeat mode %s", status)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
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

		content := fmt.Sprintf("Autoplay mode %s", status)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
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
	log.Printf("playNextInQueue started for channel: %s", channelID)
	
	url, ok := vi.GetNextFromQueue()
	if !ok {
		log.Println("No more items in queue, stopping playback")
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
		return
	}

	log.Printf("Got next URL from queue: %s", url)

	vi.Mu.Lock()
	vi.IsPlaying = true
	vi.Mu.Unlock()

	log.Printf("Sending download message to channel")
	// Send initial message
	message, err := s.ChannelMessageSend(channelID, fmt.Sprintf("Downloading: %s", url))
	if err != nil {
		log.Printf("Failed to send download message: %v", err)
	} else {
		log.Printf("Download message sent with ID: %s", message.ID)
	}

	var audioFile string

	// Determine if it's a YouTube or Spotify URL
	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
		// Extract video ID
		videoID, err := youtubeClient.GetVideoID(url)
		if err != nil {
			s.ChannelMessageSend(channelID, "❌ Invalid YouTube URL")
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
			return
		}

		// Download the audio
		audioFile, err = youtubeClient.DownloadAudio(videoID)
		if err != nil {
			s.ChannelMessageSend(channelID, fmt.Sprintf("❌ Error downloading audio: %v", err))
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
			return
		}

		// Clean up the audio file when done
		defer os.Remove(audioFile)

		// Update the message to show we're now playing
		s.ChannelMessageEdit(channelID, message.ID, fmt.Sprintf("🎵 Now playing: %s", url))

		// Play the audio file
		err = vi.PlayAudio(audioFile)
		if err != nil {
			s.ChannelMessageSend(channelID, fmt.Sprintf("❌ Error playing audio: %v", err))
		}

	} else if strings.Contains(url, "spotify.com") {
		if spotifyClient == nil {
			s.ChannelMessageSend(channelID, "❌ Spotify support is not available")
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
			return
		}

		s.ChannelMessageSend(channelID, "❌ Spotify support is not yet implemented")
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
		return
	} else {
		s.ChannelMessageSend(channelID, "❌ Unsupported URL. Please provide a YouTube or Spotify URL.")
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
		return
	}

	// Edit message to indicate track finished playing
	s.ChannelMessageEdit(channelID, message.ID, fmt.Sprintf("✅ Finished playing: %s", url))

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
			// For now, just repeat the current track
			// In a real implementation, you might want to implement a better autoplay system
			vi.Mu.Lock()
			vi.Queue = append(vi.Queue, currentURL)
			vi.Mu.Unlock()

			// Recursively call playNextInQueue to play the next item
			go playNextInQueue(s, channelID, vi)
		} else {
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
		}
	} else {
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
	}
}
