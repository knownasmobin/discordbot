package youtube

import (
	"fmt"
	"regexp"
)

// Client represents a YouTube client
type Client struct {
	Invidious *InvidiousClient
}

// NewClient creates a new YouTube client
func NewClient() *Client {
	return &Client{
		Invidious: NewInvidiousClient(),
	}
}

// GetVideoID extracts the video ID from a YouTube URL
func (c *Client) GetVideoID(url string) (string, error) {
	// Regular expressions for different YouTube URL formats
	patterns := []string{
		`(?:youtube\.com\/watch\?v=|youtu\.be\/|youtube\.com\/embed\/)([^&\n?#]+)`,
		`(?:youtube\.com\/shorts\/)([^&\n?#]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("invalid YouTube URL: %s", url)
}

// SearchVideos searches for videos using the Invidious API
func (c *Client) SearchVideos(query string, maxResults int) ([]VideoResult, error) {
	return c.Invidious.SearchVideos(query, maxResults)
}

// VideoResult represents a YouTube video search result
type VideoResult struct {
	VideoID string
	Title   string
}
