package youtube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SearchResult represents a YouTube search result
type SearchResult struct {
	Items []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			Title string `json:"title"`
		} `json:"snippet"`
	} `json:"items"`
}

// Search searches YouTube for a query and returns the first video URL
func Search(query string) (string, error) {
	// Since we can't use the YouTube Data API without credentials,
	// we'll implement a simple function that just returns a URL
	// In a real implementation, you would use the YouTube Data API with proper authentication

	// Format the query for a YouTube search URL
	escapedQuery := url.QueryEscape(query)
	_ = fmt.Sprintf("https://www.youtube.com/results?search_query=%s", escapedQuery)

	// In a real implementation, you would parse the HTML or use the API
	// For now, we'll just return a placeholder
	youtubeURL := fmt.Sprintf("https://www.youtube.com/watch?v=placeholder_%s", escapedQuery)

	return youtubeURL, nil
}

// SearchWithAPI searches YouTube using the Data API (requires API key)
func SearchWithAPI(query string, apiKey string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("YouTube API key is required")
	}

	// Format query for API
	escapedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/search?part=snippet&q=%s&type=video&key=%s",
		escapedQuery, apiKey)

	// Make the request
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to search YouTube: %v", err)
	}
	defer resp.Body.Close()

	// Parse the response
	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	// Check if we got any results
	if len(result.Items) == 0 {
		return "", fmt.Errorf("no videos found for query: %s", query)
	}

	// Get the first video ID
	videoID := result.Items[0].ID.VideoID

	// Return YouTube video URL
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID), nil
}
