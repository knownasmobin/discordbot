package youtube

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/kkdai/youtube/v2"
)

// Client handles YouTube audio downloads and streaming
type Client struct {
	YoutubeClient youtube.Client
	CacheDir      string
}

// NewClient creates a new YouTube client
func NewClient() *Client {
	cacheDir := "./cache"
	// Create cache directory if it doesn't exist
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.Mkdir(cacheDir, 0755)
	}

	return &Client{
		YoutubeClient: youtube.Client{},
		CacheDir:      cacheDir,
	}
}

// GetVideoID extracts the video ID from a YouTube URL
func (c *Client) GetVideoID(url string) (string, error) {
	// Regular expressions to match YouTube URL patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/watch\?v=([^&]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/embed/([^?]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/v/([^?]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtu\.be/([^?]+)`),
	}

	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(url); len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("invalid YouTube URL: %s", url)
}

// DownloadAudio downloads audio from a YouTube video
func (c *Client) DownloadAudio(videoID string) (string, error) {
	// Check if we have a cached file
	cachePath := filepath.Join(c.CacheDir, videoID+".pcm")
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// Get video info
	video, err := c.YoutubeClient.GetVideo(videoID)
	if err != nil {
		return "", fmt.Errorf("failed to get video info: %v", err)
	}

	// Get audio only format
	formats := video.Formats.WithAudioChannels()
	if len(formats) == 0 {
		return "", fmt.Errorf("no audio formats available")
	}

	// Get the audio stream
	stream, _, err := c.YoutubeClient.GetStream(video, &formats[0])
	if err != nil {
		return "", fmt.Errorf("failed to get audio stream: %v", err)
	}
	defer stream.Close()

	// Create a temporary file for the downloaded audio
	tmpFile := filepath.Join(c.CacheDir, videoID+".tmp")
	outFile, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer outFile.Close()

	// Copy the stream to the file
	_, err = io.Copy(outFile, stream)
	if err != nil {
		return "", fmt.Errorf("failed to download audio: %v", err)
	}

	// Convert to PCM format using FFmpeg
	err = c.convertToDiscordFormat(tmpFile, cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %v", err)
	}

	// Clean up the temporary file
	os.Remove(tmpFile)

	return cachePath, nil
}

// convertToDiscordFormat converts audio to a format that Discord can play
func (c *Client) convertToDiscordFormat(inFile, outFile string) error {
	cmd := exec.Command("ffmpeg",
		"-i", inFile,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		outFile)

	return cmd.Run()
}

// Play plays YouTube audio in a Discord voice channel
func (c *Client) Play(vc *discordgo.VoiceConnection, url string) error {
	// Extract video ID
	videoID, err := c.GetVideoID(url)
	if err != nil {
		return err
	}

	// Download audio
	audioFile, err := c.DownloadAudio(videoID)
	if err != nil {
		return err
	}

	// Start speaking
	vc.Speaking(true)
	defer vc.Speaking(false)

	// Read the file
	file, err := os.Open(audioFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a buffer for sending
	buf := make([]byte, 16*1024)
	for {
		n, err := file.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// Send the buffer
		vc.OpusSend <- buf[:n]
	}

	return nil
}
