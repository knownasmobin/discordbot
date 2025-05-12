package youtube

import (
	"fmt"
)

// SearchInvidious searches for videos using Invidious instances
func SearchInvidious(query string) (string, error) {
	// Create a new Invidious client
	client := NewInvidiousClient()

	// Search for videos with a limit of 1 (just get the top result)
	results, err := client.SearchVideos(query, 1)
	if err != nil {
		return "", fmt.Errorf("failed to search Invidious: %v", err)
	}

	// Check if we got any results
	if len(results) == 0 {
		return "", fmt.Errorf("no videos found for query: %s", query)
	}

	// Get the first video ID
	videoID := results[0].VideoID

	// Return Invidious watch URL
	return client.GetInvidiousWatchURL(videoID), nil
}

// SearchWithInvidiousAPI searches using the Invidious API with multiple instances
func SearchWithInvidiousAPI(query string, limit int) ([]InvidiousSearchResult, error) {
	// Create a new Invidious client
	client := NewInvidiousClient()

	// Search for videos
	return client.SearchVideos(query, limit)
}

// ExtractVideoIDFromURL extracts the video ID from a YouTube or Invidious URL
func ExtractVideoIDFromURL(urlStr string) (string, error) {
	// Create clients
	youtubeClient := NewClient()
	invidiousClient := NewInvidiousClient()

	// Check if it's an Invidious URL
	if invidiousClient.IsInvidiousURL(urlStr) {
		return invidiousClient.ExtractVideoIDFromInvidiousURL(urlStr)
	}

	// Otherwise, use the YouTube client's method
	return youtubeClient.GetVideoID(urlStr)
}

// GetVideoInfoFromInvidious gets video information from Invidious
func GetVideoInfoFromInvidious(videoID string) (*InvidiousVideo, error) {
	// Create a new Invidious client
	client := NewInvidiousClient()

	// Get video info
	return client.GetVideoInfo(videoID)
}
