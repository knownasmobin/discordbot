package youtube

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

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

	// Create command to download audio using yt-dlp
	cmd := exec.Command("yt-dlp",
		"-x",                      // Extract audio
		"--audio-format", "mp3",   // Convert to MP3
		"-o", outputPath,          // Output path
		"--no-playlist",           // Don't download playlists
		"--no-warnings",           // Suppress warnings
		"--quiet",                 // Quiet mode
		"--no-cache-dir",          // Don't use cache
		"--no-check-certificate",   // Skip SSL certificate verification
		"--audio-quality", "0",    // Best audio quality
		"--default-search", "auto", // Auto-detect URL type
		"--prefer-ffmpeg",         // Prefer ffmpeg for post-processing
		"--ffmpeg-location", "",   // Use system ffmpeg
		"https://youtube.com/watch?v="+videoID,
	)


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
	// Extract video ID from URL
	videoID, err := c.GetVideoID(url)
	if err != nil {
		return fmt.Errorf("invalid YouTube URL: %v", err)
	}

	// Download the audio
	audioFile, err := c.DownloadAudio(videoID)
	if err != nil {
		return fmt.Errorf("failed to download audio: %v", err)
	}
	defer os.Remove(audioFile) // Clean up the file after playing

	// Create a new FFmpeg audio source
	ffmpegCmd := exec.Command("ffmpeg",
		"-i", audioFile,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-loglevel", "warning",
		"-hide_banner",
		"-nostats",
		"pipe:1",
	)

	// Get the audio stream
	audioStream, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	// Start FFmpeg
	err = ffmpegCmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start FFmpeg: %v", err)
	}
	defer ffmpegCmd.Process.Kill()

	// Create a buffer for the audio data
	buffer := make([][]byte, 0)
	tempBuf := make([]byte, 1024)

	// Read the audio data into the buffer
	for {
		n, err := audioStream.Read(tempBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading audio stream: %v", err)
		}

		buffer = append(buffer, tempBuf[:n])
	}

	// Send the audio data to Discord
	vc.Speaking(true)
	defer vc.Speaking(false)

	// Send the audio data in chunks
	for _, chunk := range buffer {
		vc.OpusSend <- chunk
	}

	return nil
}
