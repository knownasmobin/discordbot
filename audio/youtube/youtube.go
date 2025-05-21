package youtube

import (
	"bytes"
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
		if len(parts) >= 2 {
			return strings.Split(parts[1], "?")[0], nil
		}
	}

	return "", fmt.Errorf("could not extract video ID from URL")
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

	// Create command to download audio using yt-dlp
	cmd := exec.Command("yt-dlp",
		"-x",                      // Extract audio
		"--audio-format", "best",  // Best audio quality
		"-o", outputPath,          // Output path
		"--no-playlist",           // Don't download playlists
		"--no-warnings",           // Suppress warnings
		"--quiet",                 // Quiet mode
		"--no-cache-dir",          // Don't use cache
		"--no-check-certificate",   // Skip SSL certificate verification
		"--format", "bestaudio",   // Ensure we get the best audio quality
		"--extract-audio",         // Extract audio
		"--audio-quality", "0",    // Best audio quality
		"--default-search", "auto", // Auto-detect URL type
		"--prefer-ffmpeg",         // Prefer ffmpeg for post-processing
		"--ffmpeg-location", "",   // Use system ffmpeg
		"https://youtube.com/watch?v="+videoID,
	)

	// Set up error handling
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Run the command
	log.Printf("Downloading audio for video ID: %s", videoID)
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error downloading audio: %v, stderr: %s", err, stderr.String())
	}

	// Find the downloaded file
	matches, err := filepath.Glob(filepath.Join(c.CacheDir, videoID+".*"))
	if err != nil {
		return "", fmt.Errorf("error searching for downloaded file: %v", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no matching audio files found in %s", c.CacheDir)
	}

	// Return the first matching file with absolute path
	audioPath, err := filepath.Abs(matches[0])
	if err != nil {
		return "", fmt.Errorf("error getting absolute path: %v", err)
	}

	log.Printf("Successfully downloaded audio to: %s", audioPath)
	return audioPath, nil
}

// Play plays YouTube audio in a Discord voice channel
func (c *Client) Play(vc *discordgo.VoiceConnection, url string) error {
	// First, get the video ID from the URL
	videoID, err := c.GetVideoID(url)
	if err != nil {
		return fmt.Errorf("invalid YouTube URL: %v", err)
	}

	// Download the audio
	audioPath, err := c.DownloadAudio(videoID)
	if err != nil {
		return fmt.Errorf("error downloading audio: %v", err)
	}

	// Make sure to clean up the temporary file
	defer func() {
		if audioPath != "" {
			if err := os.Remove(audioPath); err != nil {
				log.Printf("Error removing temporary file %s: %v", audioPath, err)
			}
		}
	}()

	// Set speaking state
	err = vc.Speaking(true)
	if err != nil {
		return fmt.Errorf("error setting speaking state: %v", err)
	}
	defer vc.Speaking(false)

	// Create a command to convert the audio to raw PCM and send to stdout
	cmd := exec.Command("ffmpeg",
		"-i", audioPath,           // Input file
		"-f", "s16le",              // Output format (signed 16-bit little-endian)
		"-ar", "48000",             // Audio sample rate (48kHz)
		"-ac", "2",                 // Audio channels (stereo)
		"-loglevel", "warning",      // Only show warnings and errors
		"-bufsize", "96K",           // Buffer size for better streaming
		"-threads", "0",             // Use all available CPU threads
		"pipe:1")                   // Output to stdout

	// Get the command's stdout pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error creating stdout pipe: %v", err)
	}

	// Capture stderr for debugging
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Start the command
	log.Printf("Starting ffmpeg for audio playback: %s", audioPath)
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("error starting ffmpeg: %v, stderr: %s", err, stderr.String())
	}

	// Make sure to clean up the ffmpeg process
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	// Buffer for reading audio data (20ms of stereo audio at 48kHz = 3840 bytes)
	buffer := make([]byte, 3840)

	// Read and send audio data
	for {
		// Read raw PCM data
		n, err := io.ReadFull(stdout, buffer)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		} else if err != nil {
			log.Printf("Error reading audio data: %v, stderr: %s", err, stderr.String())
			break
		}

		// Send the raw PCM data to Discord
		// Discord will handle the Opus encoding internally
		select {
		case vc.OpusSend <- buffer[:n]:
			// Data sent successfully
		default:
			// Channel full, skip this chunk to prevent blocking
			log.Println("Warning: OpusSend channel full, dropping audio data")
		}

		// Small delay to prevent overwhelming the connection
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for ffmpeg to finish
	if err := cmd.Wait(); err != nil {
		log.Printf("ffmpeg finished with error: %v, stderr: %s", err, stderr.String())
	}

	return nil
}
