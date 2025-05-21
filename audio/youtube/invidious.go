package youtube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// InvidiousClient represents a client for the Invidious API
type InvidiousClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// InvidiousVideo represents video information from Invidious API
type InvidiousVideo struct {
	VideoID       string   `json:"videoId"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Thumbnails    []string `json:"thumbnails"`
	Author        string   `json:"author"`
	LengthSeconds int      `json:"lengthSeconds"`
	ViewCount     int64    `json:"viewCount"`
	Published     int64    `json:"published"`
	LiveNow       bool     `json:"liveNow"`
	AudioStreams  []struct {
		URL      string `json:"url"`
		Quality  string `json:"quality"`
		MimeType string `json:"mimeType"`
		Bitrate  int    `json:"bitrate"`
	} `json:"audioStreams"`
}

// InvidiousSearchResult represents a search result from Invidious API
type InvidiousSearchResult struct {
	Type          string `json:"type"`
	VideoID       string `json:"videoId"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	LengthSeconds int    `json:"lengthSeconds"`
	ViewCount     int64  `json:"viewCount"`
	Published     int64  `json:"published"`
	Thumbnails    []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"thumbnails"`
}

// NewInvidiousClient creates a new Invidious client
func NewInvidiousClient() *InvidiousClient {
	return &InvidiousClient{
		BaseURL:    "https://invidious.snopyta.org",
		HTTPClient: &http.Client{},
	}
}

// GetInvidiousWatchURL returns the Invidious watch URL for a video ID
func (c *InvidiousClient) GetInvidiousWatchURL(videoID string) string {
	return fmt.Sprintf("%s/watch?v=%s", c.BaseURL, videoID)
}

// GetVideoInfo gets video information from Invidious
func (c *InvidiousClient) GetVideoInfo(videoID string) (*VideoInfo, error) {
	url := fmt.Sprintf("%s/api/v1/videos/%s", c.BaseURL, videoID)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get video info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get video info: status code %d", resp.StatusCode)
	}

	var videoInfo VideoInfo
	if err := json.NewDecoder(resp.Body).Decode(&videoInfo); err != nil {
		return nil, fmt.Errorf("failed to decode video info: %v", err)
	}

	return &videoInfo, nil
}

// SearchVideos searches for videos using the Invidious API
func (c *InvidiousClient) SearchVideos(query string, maxResults int) ([]VideoResult, error) {
	searchURL := fmt.Sprintf("%s/api/v1/search?q=%s&type=video&sort_by=relevance",
		c.BaseURL,
		url.QueryEscape(query))

	resp, err := c.HTTPClient.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search videos: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to search videos: status code %d", resp.StatusCode)
	}

	var results []VideoResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to decode search results: %v", err)
	}

	// Limit results to maxResults
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

// VideoInfo represents video information from Invidious
type VideoInfo struct {
	Title         string `json:"title"`
	VideoID       string `json:"videoId"`
	Author        string `json:"author"`
	Description   string `json:"description"`
	LengthSeconds int    `json:"lengthSeconds"`
}

// GetAudioStreamURL returns the best audio stream URL for a video
func (ic *InvidiousClient) GetAudioStreamURL(videoID string) (string, error) {
	video, err := ic.GetVideoInfo(videoID)
	if err != nil {
		return "", err
	}

	if len(video.AudioStreams) == 0 {
		return "", fmt.Errorf("no audio streams available for video %s", videoID)
	}

	// Find the highest quality audio stream
	var bestStream struct {
		URL     string
		Bitrate int
	}

	for _, stream := range video.AudioStreams {
		// Skip streams with no URL
		if stream.URL == "" {
			continue
		}

		// Check if this stream has a higher bitrate than our current best
		if stream.Bitrate > bestStream.Bitrate {
			bestStream.URL = stream.URL
			bestStream.Bitrate = stream.Bitrate
		}
	}

	if bestStream.URL == "" {
		return "", fmt.Errorf("no valid audio stream URL found for video %s", videoID)
	}

	return bestStream.URL, nil
}

// IsInvidiousURL checks if a URL is from an Invidious instance
func (ic *InvidiousClient) IsInvidiousURL(urlStr string) bool {
	return strings.HasPrefix(urlStr, ic.BaseURL)
}

// ExtractVideoIDFromInvidiousURL extracts the video ID from an Invidious URL
func (ic *InvidiousClient) ExtractVideoIDFromInvidiousURL(urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	// Extract the video ID from the query parameters
	query := parsedURL.Query()
	videoID := query.Get("v")
	if videoID == "" {
		return "", fmt.Errorf("no video ID found in URL: %s", urlStr)
	}

	return videoID, nil
}
