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

	// Register the messageCreate func as a callback for MessageCreate events
	discord.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening
	err = discord.Open()
	if err != nil {
		log.Fatal("Error opening connection: ", err)
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

	// Cleanly close down the Discord session
	discord.Close()
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Check if the message is a command
	if !strings.HasPrefix(m.Content, "!") {
		return
	}

	// Parse the command
	parts := strings.Fields(m.Content)
	command := strings.TrimPrefix(parts[0], "!")

	// Get the voice instance for this guild
	vi := voiceManager.GetVoiceInstance(m.GuildID)

	// Handle commands
	switch command {
	case "ping":
		s.ChannelMessageSend(m.ChannelID, "Pong!")

	case "join":
		// Check if the user is in a voice channel
		vs, err := findUserVoiceState(s, m.GuildID, m.Author.ID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "You need to be in a voice channel first!")
			return
		}

		// Join the user's voice channel
		err = vi.Join(s, vs.ChannelID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error joining voice channel: %v", err))
			return
		}

		s.ChannelMessageSend(m.ChannelID, "Joined voice channel!")

	case "leave":
		// Leave the voice channel if we're in one
		if vi.Connection == nil {
			s.ChannelMessageSend(m.ChannelID, "I'm not in a voice channel!")
			return
		}

		err := vi.Leave()
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error leaving voice channel: %v", err))
			return
		}

		s.ChannelMessageSend(m.ChannelID, "Left voice channel!")

	case "play":
		if len(parts) < 2 {
			s.ChannelMessageSend(m.ChannelID, "Please provide a URL to play")
			return
		}
		url := parts[1]

		// Check if we're in a voice channel
		if vi.Connection == nil {
			// Try to join the user's voice channel
			vs, err := findUserVoiceState(s, m.GuildID, m.Author.ID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "You need to be in a voice channel first!")
				return
			}

			// Join the user's voice channel
			err = vi.Join(s, vs.ChannelID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error joining voice channel: %v", err))
				return
			}
		}

		// Add the URL to the queue
		vi.AddToQueue(url)
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Added to queue: %s", url))

		// If nothing is playing, start playing
		if !vi.IsPlaying {
			go playNextInQueue(s, m.ChannelID, vi)
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
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Repeat mode %s", status))

	case "autoplay":
		// Toggle autoplay mode
		vi.Mu.Lock()
		vi.Autoplay = !vi.Autoplay
		status := "enabled"
		if !vi.Autoplay {
			status = "disabled"
		}
		vi.Mu.Unlock()
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Autoplay mode %s", status))

	case "queue":
		if len(parts) < 2 {
			// Show the current queue
			vi.Mu.Lock()
			if len(vi.Queue) == 0 {
				s.ChannelMessageSend(m.ChannelID, "The queue is empty")
			} else {
				queueMsg := "Current queue:\n"
				for i, url := range vi.Queue {
					queueMsg += fmt.Sprintf("%d. %s\n", i+1, url)
				}
				s.ChannelMessageSend(m.ChannelID, queueMsg)
			}
			vi.Mu.Unlock()
		} else {
			// Add URL to queue
			url := parts[1]
			vi.AddToQueue(url)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Added to queue: %s", url))

			// If nothing is playing, start playing
			if !vi.IsPlaying {
				// Check if we're in a voice channel
				if vi.Connection == nil {
					// Try to join the user's voice channel
					vs, err := findUserVoiceState(s, m.GuildID, m.Author.ID)
					if err != nil {
						s.ChannelMessageSend(m.ChannelID, "You need to be in a voice channel first!")
						return
					}

					// Join the user's voice channel
					err = vi.Join(s, vs.ChannelID)
					if err != nil {
						s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error joining voice channel: %v", err))
						return
					}
				}

				// Start playing the queue
				go playNextInQueue(s, m.ChannelID, vi)
			}
		}
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

	var err error
	// Determine if it's a YouTube or Spotify URL
	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
		s.ChannelMessageSend(channelID, fmt.Sprintf("Playing YouTube: %s", url))
		err = youtubeClient.Play(vi.Connection, url)
	} else if strings.Contains(url, "spotify.com") {
		if spotifyClient == nil {
			s.ChannelMessageSend(channelID, "Spotify support is not available")
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
			return
		}

		s.ChannelMessageSend(channelID, fmt.Sprintf("Playing Spotify: %s", url))
		err = spotifyClient.PlayTrack(vi.Connection, url)
	} else {
		s.ChannelMessageSend(channelID, "Unsupported URL. Please provide a YouTube or Spotify URL.")
		vi.Mu.Lock()
		vi.IsPlaying = false
		vi.Mu.Unlock()
		return
	}

	if err != nil {
		s.ChannelMessageSend(channelID, fmt.Sprintf("Error playing: %v", err))
	}

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
