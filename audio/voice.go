package audio

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
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

// Cleanup cleans up all voice connections with a timeout
func (vm *VoiceManager) Cleanup() {
	vm.Mu.Lock()
	defer vm.Mu.Unlock()

	// Create a channel to track cleanup completion
	done := make(chan struct{})
	
	// Start cleanup in a goroutine
	go func() {
		defer close(done)
		for guildID, instance := range vm.Instances {
			if instance != nil {
				log.Printf("Cleaning up voice instance for guild %s", guildID)
				if err := instance.Leave(); err != nil {
					log.Printf("Error cleaning up voice instance for guild %s: %v", guildID, err)
				}
				delete(vm.Instances, guildID)
			}
		}
	}()

	// Wait for cleanup to complete or timeout
	select {
	case <-done:
		log.Println("Voice manager cleanup completed")
	case <-time.After(5 * time.Second):
		log.Println("Warning: Voice manager cleanup timed out")
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
		// Use a non-blocking send with a timeout
		select {
		case vi.StopChan <- true:
			log.Println("Sent stop signal to audio player")
		case <-time.After(100 * time.Millisecond):
			log.Println("Timeout sending stop signal, continuing with disconnect")
		}

		// Give a short time for the audio to stop
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()

		// Reset the stop channel
		close(vi.StopChan)
		vi.StopChan = make(chan bool, 1)
	}

	// Disconnect from voice with a timeout
	done := make(chan struct{})
	var err error

	go func() {
		err = vi.Connection.Disconnect()
		close(done)
	}()

	select {
	case <-done:
		// Disconnect completed
	case <-time.After(2 * time.Second):
		log.Println("Timeout disconnecting from voice, forcing close")
	}

	if err != nil {
		log.Printf("Error disconnecting from voice: %v", err)
	}

	// Clean up resources
	vi.Connection = nil
	vi.ChannelID = ""
	vi.IsPlaying = false
	vi.CurrentURL = ""
	vi.Queue = nil

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
			"-af", "volume=0.5,aresample=async=1000", // Adjust volume and resample
			"-acodec", "pcm_s16le",    // Force PCM signed 16-bit little-endian codec
			"-ar", "48000",            // Force 48kHz sample rate
			"-ac", "2",                // Force stereo
			"-f", "s16le",             // Force output format
			"-fflags", "nobuffer",     // Reduce input buffering
			"-flags", "low_delay",     // Reduce latency
			"-probesize", "32",        // Reduce probe size
			"-analyzeduration", "0",   // Don't analyze the entire file
			"pipe:1")                  // Output to stdout

		// Create a buffer for reading audio data (60ms of stereo audio at 48kHz = 11520 bytes)
		// Using a larger buffer to reduce the number of reads
		buffer := make([]byte, 11520)

		// Get the command's stdout pipe
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Error creating stdout pipe: %v", err)
			return
		}

		// Set process group ID to allow killing child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		// Start the command
		err = cmd.Start()
		if err != nil {
			log.Printf("Error starting ffmpeg: %v", err)
			return
		}

		// Make sure to clean up the ffmpeg process
		defer func() {
			if cmd.Process != nil {
				// Kill the entire process group
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}()

		// Create a ticker for consistent timing (60ms = ~16.67fps)
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Read raw PCM data with timeout
				n, err := stdout.Read(buffer)
				if err == io.EOF {
					// End of file, we're done
					return
				} else if err != nil {
					log.Printf("Error reading audio data: %v", err)
					return
				}

				// Only send if we have data
				if n > 0 {
					// Copy the buffer to ensure we don't modify it while it's being sent
					frame := make([]byte, n)
					copy(frame, buffer[:n])

					// Send the frame with a timeout
					select {
					case vi.Connection.OpusSend <- frame:
						// Frame sent successfully
					case <-time.After(100 * time.Millisecond):
						// Skip frame if we can't send it in time
						log.Println("Warning: Frame send timeout, dropping frame")
					}
				}
			}
		}
	}()

	return nil
}
