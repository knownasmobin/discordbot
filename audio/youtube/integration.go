package youtube

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bwmarrin/discordgo"
)

// YouTubeInvidiousIntegration integrates the YouTube client with Invidious
type YouTubeInvidiousIntegration struct {
	YouTubeClient   *Client
	InvidiousClient *InvidiousClient
}

// NewYouTubeInvidiousIntegration creates a new integration between YouTube and Invidious
func NewYouTubeInvidiousIntegration() *YouTubeInvidiousIntegration {
	return &YouTubeInvidiousIntegration{
		YouTubeClient:   NewClient(),
		InvidiousClient: NewInvidiousClient(),
	}
}

// GetVideoID extracts the video ID from a YouTube or Invidious URL
func (i *YouTubeInvidiousIntegration) GetVideoID(urlStr string) (string, error) {
	// First check if it's an Invidious URL
	if i.InvidiousClient.IsInvidiousURL(urlStr) {
		return i.InvidiousClient.ExtractVideoIDFromInvidiousURL(urlStr)
	}

	// Otherwise, use the YouTube client's method
	return i.YouTubeClient.GetVideoID(urlStr)
}

// DownloadAudio downloads audio from YouTube or Invidious
func (i *YouTubeInvidiousIntegration) DownloadAudio(videoID string) (string, error) {
	// First try to get the audio stream URL from Invidious
	fmt.Printf("Attempting to get audio stream from Invidious for video ID: %s\n", videoID)
	audioURL, err := i.InvidiousClient.GetAudioStreamURL(videoID)
	if err == nil && audioURL != "" {
		fmt.Printf("Successfully got audio stream URL from Invidious: %s\n", audioURL)

		// Create cache directory if it doesn't exist
		if err := i.YouTubeClient.ensureCacheDir(); err != nil {
			return "", fmt.Errorf("failed to create cache directory: %v", err)
		}

		// Download the audio stream directly
		cachePath, err := i.YouTubeClient.downloadAndConvertAudioStream(audioURL, videoID)
		if err == nil {
			return cachePath, nil
		}

		fmt.Printf("Failed to download audio stream from Invidious: %v\nFalling back to YouTube...\n", err)
	} else {
		fmt.Printf("Failed to get audio stream URL from Invidious: %v\nFalling back to YouTube...\n", err)
	}

	// Fallback to the YouTube client's method
	return i.YouTubeClient.DownloadAudio(videoID)
}

// Play plays audio in a Discord voice channel
func (i *YouTubeInvidiousIntegration) Play(vc *discordgo.VoiceConnection, urlStr string) error {
	// Extract video ID from either YouTube or Invidious URL
	videoID, err := i.GetVideoID(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}

	// Use the YouTube client's Play method with the extracted video ID
	return i.YouTubeClient.Play(vc, fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID))
}

// Search searches for videos on YouTube or Invidious
func (i *YouTubeInvidiousIntegration) Search(query string, limit int) (string, error) {
	// First try to search using Invidious
	fmt.Printf("Searching for '%s' using Invidious\n", query)
	results, err := i.InvidiousClient.SearchVideos(query, limit)
	if err == nil && len(results) > 0 {
		// Return the first result's video URL
		videoID := results[0].VideoID
		fmt.Printf("Found video on Invidious: %s by %s\n", results[0].Title, results[0].Author)
		return i.InvidiousClient.GetInvidiousWatchURL(videoID), nil
	}

	// Fallback to YouTube search
	fmt.Printf("Invidious search failed: %v\nFalling back to YouTube search...\n", err)
	return Search(query)
}

// IsInvidiousURL checks if a URL is from an Invidious instance
func (i *YouTubeInvidiousIntegration) IsInvidiousURL(urlStr string) bool {
	return i.InvidiousClient.IsInvidiousURL(urlStr)
}

// ensureCacheDir ensures the cache directory exists
func (c *Client) ensureCacheDir() error {
	if c.CacheDir == "" {
		c.CacheDir = "./cache"
	}
	return os.MkdirAll(c.CacheDir, 0755)
}

// downloadAndConvertAudioStream downloads an audio stream and converts it to Discord format
func (c *Client) downloadAndConvertAudioStream(audioURL string, videoID string) (string, error) {
	// Create temporary files
	tmpFile := filepath.Join(c.CacheDir, videoID+".tmp")
	cachePath := filepath.Join(c.CacheDir, videoID+".pcm")

	// Check if the file is already in cache
	if _, err := os.Stat(cachePath); err == nil {
		fmt.Printf("Found cached audio file: %s\n", cachePath)
		return cachePath, nil
	}

	// Download the audio stream
	fmt.Printf("Downloading audio stream: %s\n", audioURL)
	resp, err := http.Get(audioURL)
	if err != nil {
		return "", fmt.Errorf("failed to download audio stream: %v", err)
	}
	defer resp.Body.Close()

	// Create the temporary file
	tmpFileHandle, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer tmpFileHandle.Close()

	// Copy the audio stream to the temporary file
	_, err = io.Copy(tmpFileHandle, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to save audio stream: %v", err)
	}

	// Convert to Discord format
	err = c.convertToDiscordFormat(tmpFile, cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %v", err)
	}

	// Clean up the temporary file
	os.Remove(tmpFile)
	return cachePath, nil
}
