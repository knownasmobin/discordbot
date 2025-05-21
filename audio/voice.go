package audio

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// VoiceInstance represents a voice connection to a Discord guild
type VoiceInstance struct {
	GuildID    string
	ChannelID  string
	Connection *discordgo.VoiceConnection
	IsPlaying  bool
	Repeat     bool
	Autoplay   bool
	CurrentURL string
	Queue      []string
	Mu         sync.Mutex
	StopChan   chan bool
}

// VoiceManager manages voice connections
type VoiceManager struct {
	Instances map[string]*VoiceInstance
	Mu        sync.Mutex
}

// NewVoiceManager creates a new voice manager
func NewVoiceManager() *VoiceManager {
	return &VoiceManager{
		Instances: make(map[string]*VoiceInstance),
	}
}

// GetVoiceInstance gets or creates a voice instance for a guild
func (vm *VoiceManager) GetVoiceInstance(guildID string) *VoiceInstance {
	vm.Mu.Lock()
	defer vm.Mu.Unlock()

	if instance, exists := vm.Instances[guildID]; exists {
		return instance
	}

	instance := &VoiceInstance{
		GuildID:  guildID,
		StopChan: make(chan bool),
	}
	vm.Instances[guildID] = instance
	return instance
}

// Join connects to a voice channel
func (vi *VoiceInstance) Join(s *discordgo.Session, channelID string) error {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()

	log.Printf("Attempting to join voice channel %s in guild %s", channelID, vi.GuildID)

	// If we're already connected to this channel, do nothing
	if vi.Connection != nil && vi.ChannelID == channelID {
		log.Printf("Already connected to voice channel %s", channelID)
		return nil
	}

	// Initialize StopChan if it's nil
	if vi.StopChan == nil {
		vi.StopChan = make(chan bool, 1)
	}

	// Disconnect from current channel if we're in one
	if vi.Connection != nil {
		log.Printf("Leaving current voice channel %s", vi.ChannelID)
		// Don't use vi.Leave() here to avoid deadlock
		if err := vi.Connection.Disconnect(); err != nil {
			log.Printf("Error disconnecting from current channel: %v", err)
		}
		vi.Connection = nil
	}

	// Connect to the new channel
	log.Printf("Connecting to voice channel %s", channelID)
	vc, err := s.ChannelVoiceJoin(vi.GuildID, channelID, false, true)
	if err != nil {
		log.Printf("Failed to join voice channel: %v", err)
		return fmt.Errorf("failed to join voice channel: %v", err)
	}

	// Initialize voice connection properties
	vi.Connection = vc
	vi.ChannelID = channelID

	// Wait for voice connection to be ready
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if vc.Ready {
				log.Printf("Successfully connected to voice channel %s", channelID)
				return nil
			}
		case <-timeout:
			log.Printf("Timed out waiting for voice connection to be ready")
			// Clean up the failed connection
			if err := vc.Disconnect(); err != nil {
				log.Printf("Error cleaning up failed voice connection: %v", err)
			}
			vi.Connection = nil
			vi.ChannelID = ""
			return fmt.Errorf("timed out waiting for voice connection to be ready")
		}
	}
}

// Leave disconnects from a voice channel and cleans up resources
func (vi *VoiceInstance) Leave() error {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()

	if vi.Connection == nil {
		return nil // Already disconnected
	}

	log.Printf("Disconnecting from voice channel %s in guild %s", vi.ChannelID, vi.GuildID)

	// Stop any playing audio
	if vi.StopChan != nil {
		select {
		case vi.StopChan <- true:
			// Successfully sent stop signal
		default:
			// Channel is full or closed, no need to block
		}
	}

	// Disconnect from voice
	err := vi.Connection.Disconnect()
	if err != nil {
		log.Printf("Error disconnecting from voice: %v", err)
	}

	// Clean up resources
	vi.Connection = nil
	vi.ChannelID = ""
	vi.IsPlaying = false
	vi.CurrentURL = ""
	vi.Queue = nil

	// Reset stop channel
	if vi.StopChan != nil {
		close(vi.StopChan)
		vi.StopChan = make(chan bool, 1)
	}

	log.Printf("Successfully left voice channel in guild %s", vi.GuildID)
	return nil
}

// AddToQueue adds a URL to the queue
func (vi *VoiceInstance) AddToQueue(url string) {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()
	vi.Queue = append(vi.Queue, url)
}

// GetNextFromQueue gets the next item from the queue
func (vi *VoiceInstance) GetNextFromQueue() (string, bool) {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()

	if len(vi.Queue) == 0 {
		return "", false
	}

	url := vi.Queue[0]
	vi.Queue = vi.Queue[1:]
	vi.CurrentURL = url
	return url, true
}

// PlayAudio plays audio from a file using ffmpeg to convert and play the audio
func (vi *VoiceInstance) PlayAudio(filePath string) error {
	vi.Mu.Lock()

	if vi.Connection == nil {
		vi.Mu.Unlock()
		return errors.New("not connected to a voice channel")
	}

	vi.IsPlaying = true
	vi.Mu.Unlock()

	go func() {
		defer func() {
			vi.Mu.Lock()
			vi.IsPlaying = false
			vi.Mu.Unlock()
		}()

		// Set speaking state
		err := vi.Connection.Speaking(true)
		if err != nil {
			log.Printf("Error setting speaking state: %v", err)
			return
		}
		defer vi.Connection.Speaking(false)

		// Create a command to convert the audio to raw PCM and send to stdout
		cmd := exec.Command("ffmpeg",
			"-i", filePath,           // Input file
			"-f", "s16le",            // Output format (signed 16-bit little-endian)
			"-ar", "48000",           // Audio sample rate (48kHz)
			"-ac", "2",               // Audio channels (stereo)
			"-loglevel", "warning",    // Only show warnings and errors
			"pipe:1")                 // Output to stdout

		// Get the command's stdout pipe
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Error creating stdout pipe: %v", err)
			return
		}

		// Start the command
		err = cmd.Start()
		if err != nil {
			log.Printf("Error starting ffmpeg: %v", err)
			return
		}

		// Make sure to clean up the ffmpeg process
		defer func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}()

		// Buffer for reading audio data (20ms of stereo audio at 48kHz = 3840 bytes)
		buffer := make([]byte, 3840)

		for {
			// Read raw PCM data
			n, err := io.ReadFull(stdout, buffer)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			} else if err != nil {
				log.Printf("Error reading audio data: %v", err)
				break
			}

			// Send the raw PCM data to Discord
			// Discord will handle the Opus encoding internally
			vi.Connection.OpusSend <- buffer[:n]
			// Small delay to prevent overwhelming the connection
			time.Sleep(20 * time.Millisecond)
		}
	}()

	return nil
}
