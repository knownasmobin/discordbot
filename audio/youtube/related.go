package youtube

import (
	"fmt"
	"strings"
)

// RelatedVideosResult represents the YouTube related videos API response
type RelatedVideosResult struct {
	Items []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			Title string `json:"title"`
		} `json:"snippet"`
	} `json:"items"`
}

// GetRelatedVideo returns a related video URL based on the current video URL using Invidious
func (c *Client) GetRelatedVideo(videoURL string) (string, error) {
	// Extract video ID
	videoID, err := c.GetVideoID(videoURL)
	if err != nil {
		return "", err
	}

	// Get video info from Invidious to find related videos
	video, err := c.Invidious.GetVideoInfo(videoID)
	if err != nil {
		return "", fmt.Errorf("failed to get video info from Invidious: %v", err)
	}

	// Search for similar videos using the video title
	results, err := c.SearchVideos(video.Title, 5)
	if err != nil {
		return "", fmt.Errorf("failed to search for related videos: %v", err)
	}

	// Find a video that isn't the original one
	for _, result := range results {
		if result.VideoID != videoID {
			// Return the Invidious watch URL
			return c.Invidious.GetInvidiousWatchURL(result.VideoID), nil
		}
	}

	return "", fmt.Errorf("no related videos found")
}

// GetRelatedSpotifyTrack returns a related track for Spotify URLs by searching through Invidious
func (c *Client) GetRelatedSpotifyTrack(artistName string, trackName string) (string, error) {
	// For Spotify tracks, search for other songs by the same artist
	searchQuery := fmt.Sprintf("%s similar to %s", artistName, trackName)
	fmt.Printf("Searching for related tracks to '%s' by '%s' using Invidious\n", trackName, artistName)

	// Search using Invidious
	results, err := c.SearchVideos(searchQuery, 10)
	if err != nil {
		return "", fmt.Errorf("failed to search for related tracks: %v", err)
	}

	// Filter for tracks that don't contain the original track name
	for _, result := range results {
		if !strings.Contains(strings.ToLower(result.Title), strings.ToLower(trackName)) {
			// Return the Invidious watch URL
			return c.Invidious.GetInvidiousWatchURL(result.VideoID), nil
		}
	}

	// If no suitable track found, try searching just for the artist
	if len(results) == 0 {
		artistResults, err := c.SearchVideos(artistName, 5)
		if err != nil {
			return "", fmt.Errorf("failed to search for artist tracks: %v", err)
		}

		if len(artistResults) > 0 {
			return c.Invidious.GetInvidiousWatchURL(artistResults[0].VideoID), nil
		}
	}

	return "", fmt.Errorf("no related tracks found")
}
