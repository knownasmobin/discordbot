package youtube

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Client handles YouTube audio downloads and streaming
type Client struct {
	CacheDir  string
	mu        sync.Mutex
	lastError error
}

// NewClient creates a new YouTube client
func NewClient(cacheDir string) *Client {
	if cacheDir == "" {
		cacheDir = "/tmp/discordbot/cache"
	}
	return &Client{
		CacheDir: cacheDir,
	}
}

// VideoInfo represents basic video information
type VideoInfo struct {
	ID      string
	Title   string
	Author  string
	Webpage string
}

// GetVideoID extracts the video ID from a YouTube URL
func (c *Client) GetVideoID(url string) (string, error) {
	// Handle youtu.be links
	if strings.Contains(url, "youtu.be/") {
		parts := strings.Split(url, "youtu.be/")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid YouTube URL")
		}
		return strings.Split(parts[1], "?")[0], nil
	}

	// Handle youtube.com/watch?v= links
	if strings.Contains(url, "v=") {
		parts := strings.Split(url, "v=")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid YouTube URL")
		}
		return strings.Split(parts[1], "&")[0], nil
	}

	// Handle youtu.be/ format without https://
	if strings.HasPrefix(url, "youtu.be/") {
		return strings.Split(url[9:], "?")[0], nil
	}

	// Handle youtube.com/shorts/ format
	if strings.Contains(url, "youtube.com/shorts/") {
		parts := strings.Split(url, "shorts/")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid YouTube Shorts URL")
		}
		return strings.Split(parts[1], "?")[0], nil
	}

	// If we get here, the URL format is not recognized
	return "", fmt.Errorf("unrecognized YouTube URL format")
}

// DownloadAudio downloads audio from YouTube using yt-dlp
func (c *Client) DownloadAudio(videoID string) (string, error) {
	if c.CacheDir == "" {
		c.CacheDir = "/tmp/discordbot/cache"
	}

	// Ensure cache directory exists with proper permissions
	err := os.MkdirAll(c.CacheDir, 0755)
	if err != nil {
		return "", fmt.Errorf("error creating cache directory: %v", err)
	}

	outputPath := filepath.Join(c.CacheDir, fmt.Sprintf("%s.%%(ext)s", videoID))

	// Get cookie file path from environment
	cookieFile := os.Getenv("YT_COOKIE_FILE")

	// Create base command
	args := []string{
		"-x",                    // Extract audio
		"--audio-format", "mp3", // Convert to MP3
		"-o", outputPath, // Output path
		"--no-playlist",          // Don't download playlists
		"--no-warnings",          // Suppress warnings
		"--quiet",                // Quiet mode
		"--no-cache-dir",         // Don't use cache
		"--no-check-certificate", // Skip SSL certificate verification
		"--audio-quality", "0",   // Best audio quality
		"--default-search", "auto", // Auto-detect URL type
		"--prefer-ffmpeg",                                                                       // Prefer ffmpeg for post-processing
		"--ffmpeg-location", "/home/ec2-user/discordbot/ffmpeg-n6.1-latest-linux64-gpl-6.1/bin", // Use system ffmpeg
	}

	// Add cookie file if specified
	if cookieFile != "" {
		if _, err := os.Stat(cookieFile); err == nil {
			args = append(args, "--cookies", cookieFile)
		} else {
			log.Printf("Warning: Cookie file not found at %s", cookieFile)
		}
	}

	// Add the video URL
	args = append(args, "https://youtube.com/watch?v="+videoID)

	// Create command with arguments
	cmd := exec.Command("yt-dlp", args...)

	// Run the command and capture combined output (stdout + stderr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("yt-dlp failed: %v\nOutput: %s", err, string(output))
	}

	// The actual output file will have the .mp3 extension
	actualFile := filepath.Join(c.CacheDir, fmt.Sprintf("%s.mp3", videoID))
	if _, err := os.Stat(actualFile); os.IsNotExist(err) {
		return "", fmt.Errorf("output file not found: %s", actualFile)
	}

	return actualFile, nil
}

// Play plays YouTube audio in a Discord voice channel
func (c *Client) Play(vc *discordgo.VoiceConnection, url string) error {
	log.Printf("Play called with URL: %s", url)

	if vc == nil {
		err := fmt.Errorf("voice connection is nil")
		log.Printf("Play error: %v", err)
		return err
	}

	videoID, err := c.GetVideoID(url)
	if err != nil {
		err = fmt.Errorf("invalid YouTube URL: %v", err)
		log.Printf("GetVideoID error: %v", err)
		return err
	}
	log.Printf("Extracted video ID: %s", videoID)

	// Download the audio file
	log.Printf("Starting audio download for video ID: %s", videoID)
	audioFile, err := c.DownloadAudio(videoID)
	if err != nil {
		err = fmt.Errorf("error downloading audio: %v", err)
		log.Printf("DownloadAudio error: %v", err)
		return err
	}
	log.Printf("Successfully downloaded audio to: %s", audioFile)
	defer os.Remove(audioFile) // Clean up the file after playing

	// Create a new FFmpeg command to convert the audio to Opus
	ffmpegArgs := []string{
		"-i", audioFile,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-loglevel", "warning",
		"-acodec", "libopus",
		"-f", "opus",
		"-ar", "48000",
		"-ac", "2",
		"-b:a", "128k",
		"-frame_duration", "20",
		"-application", "audio",
		"-vbr", "on",
		"-compression_level", "10",
		"-packet_loss", "1",
		"-fec", "on",
		"-dither_method", "triangular",
		"-vbr", "on",
		"-compression_level", "10",
		"-application", "audio",
		"-frame_duration", "20",
		"-packet_loss", "1",
		"-fec", "on",
		"-dither_method", "triangular",
		"pipe:1",
	}

	log.Printf("Starting FFmpeg with args: %v", ffmpegArgs)
	ffmpegCmd := exec.Command("ffmpeg", ffmpegArgs...)

	// Get the audio stream
	audioStream, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create stdout pipe: %v", err)
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	// Start FFmpeg
	log.Printf("Starting FFmpeg process")
	err = ffmpegCmd.Start()
	if err != nil {
		log.Printf("Failed to start FFmpeg: %v", err)
		return fmt.Errorf("failed to start FFmpeg: %v", err)
	}

	// Cleanup function
	cleanup := func() {
		log.Printf("Cleaning up FFmpeg process")
		if ffmpegCmd.Process != nil {
			ffmpegCmd.Process.Kill()
		}
	}
	defer cleanup()

	// Play the audio file using dgvoice
	log.Printf("Starting audio playback")
	
	// Set speaking state
	err = vc.Speaking(true)
	if err != nil {
		log.Printf("Error setting speaking state: %v", err)
		return fmt.Errorf("error setting speaking state: %v", err)
	}
	defer vc.Speaking(false)

	// Create a done channel to signal when playback is complete
	done := make(chan bool)

	// Start a goroutine to handle playback
	go func() {
		defer close(done)
		
		// Buffer for reading audio data
		const frameSize = 960 // 20ms of 48kHz stereo audio (48000 * 2 * 2 * 0.02 / 4)
		buffer := make([]byte, frameSize)
		totalBytes := 0
		startTime := time.Now()

		for {
			// Read audio data from FFmpeg
			n, err := audioStream.Read(buffer)
			if err == io.EOF {
				log.Printf("Reached end of audio stream")
				return
			}
			if err != nil {
				log.Printf("Error reading audio stream: %v", err)
				return
			}

			// Only process complete frames
			if n == 0 {
				continue
			}

			totalBytes += n

			// Send the audio data to Discord
			select {
			case vc.OpusSend <- buffer[:n]:
				// Log progress every second
				if totalBytes%(frameSize*50) == 0 { // ~1 second of audio (50 frames)
					elapsed := time.Since(startTime).Seconds()
					log.Printf("Sent %d KB (%.1f KB/s)",
						totalBytes/1024,
						float64(totalBytes)/1024/elapsed)
				}

			case <-time.After(5 * time.Second):
				log.Printf("Warning: Timeout waiting to send audio data")
				return
			}

			// Small delay to prevent overwhelming the connection
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Wait for playback to complete or connection to close
	select {
	case <-done:
		log.Printf("Playback completed")
	case <-time.After(30 * time.Minute): // Safety timeout
		log.Printf("Playback timed out after 30 minutes")
	}

	// Note: Removed the final log statement since we're now tracking progress in the goroutine

	// Small delay to ensure all data is sent
	time.Sleep(100 * time.Millisecond)

	return nil
}
