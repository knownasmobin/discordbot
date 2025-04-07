package youtube

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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

// GetRelatedVideo returns a related video URL based on the current video URL
func (c *Client) GetRelatedVideo(videoURL string) (string, error) {
	// Extract video ID
	videoID, err := c.GetVideoID(videoURL)
	if err != nil {
		return "", err
	}

	// If we have an API key, use the YouTube Data API
	apiKey := "" // Set this from env if available
	if apiKey != "" {
		return getRelatedVideoAPI(videoID, apiKey)
	}

	// Otherwise use a scraping approach
	return getRelatedVideoScrape(videoID)
}

// getRelatedVideoAPI gets related videos using the YouTube Data API
func getRelatedVideoAPI(videoID, apiKey string) (string, error) {
	apiURL := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/search?part=snippet&relatedToVideoId=%s&type=video&maxResults=1&key=%s",
		videoID, apiKey,
	)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch related videos: %v", err)
	}
	defer resp.Body.Close()

	var result RelatedVideosResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	if len(result.Items) == 0 {
		return "", fmt.Errorf("no related videos found")
	}

	relatedVideoID := result.Items[0].ID.VideoID
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", relatedVideoID), nil
}

// getRelatedVideoScrape gets related videos by scraping YouTube
// This is a fallback method when API key is not available
func getRelatedVideoScrape(videoID string) (string, error) {
	// Request the YouTube video page
	resp, err := http.Get(fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID))
	if err != nil {
		return "", fmt.Errorf("failed to fetch video page: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	// Extract related video IDs using regex
	// This is a simplified approach and might break if YouTube changes their HTML structure
	pattern := regexp.MustCompile(`"videoId":"([^"]+)"`)
	matches := pattern.FindAllSubmatch(body, -1)

	// Filter out the original video ID and duplicates
	seenIDs := map[string]bool{videoID: true}
	var relatedIDs []string

	for _, match := range matches {
		if len(match) >= 2 {
			id := string(match[1])
			if !seenIDs[id] {
				seenIDs[id] = true
				relatedIDs = append(relatedIDs, id)
			}
		}
	}

	if len(relatedIDs) == 0 {
		return "", fmt.Errorf("no related videos found")
	}

	// Return the first related video
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", relatedIDs[0]), nil
}

// GetRelatedSpotifyTrack returns a related track for Spotify URLs by converting to YouTube search
func (c *Client) GetRelatedSpotifyTrack(artistName string, trackName string) (string, error) {
	// For Spotify tracks, search for other songs by the same artist
	searchQuery := url.QueryEscape(fmt.Sprintf("%s similar to %s", artistName, trackName))
	searchURL := fmt.Sprintf("https://www.youtube.com/results?search_query=%s", searchQuery)

	// Make a request to the search page
	resp, err := http.Get(searchURL)
	if err != nil {
		return "", fmt.Errorf("failed to search: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	// Extract video IDs
	pattern := regexp.MustCompile(`"videoId":"([^"]+)"`)
	matches := pattern.FindAllSubmatch(body, -1)

	// Filter for music videos
	var videoIDs []string
	seenIDs := make(map[string]bool)

	for _, match := range matches {
		if len(match) >= 2 {
			id := string(match[1])
			if !seenIDs[id] && !strings.Contains(id, trackName) { // Avoid the same track
				seenIDs[id] = true
				videoIDs = append(videoIDs, id)
			}
		}
	}

	if len(videoIDs) == 0 {
		return "", fmt.Errorf("no related tracks found")
	}

	// Return the first result
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoIDs[0]), nil
}
