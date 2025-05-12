package youtube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// InvidiousClient handles interactions with Invidious instances
type InvidiousClient struct {
	Instances  []string
	httpClient *http.Client
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

// NewInvidiousClient creates a new Invidious client with default instances
func NewInvidiousClient() *InvidiousClient {
	return &InvidiousClient{
		Instances: []string{
			"https://yewtu.be",
			"https://invidious.snopyta.org",
			"https://invidious.kavin.rocks",
			"https://vid.puffyan.us",
			"https://inv.riverside.rocks",
			"https://invidio.xamh.de",
		},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetVideoInfo retrieves video information from Invidious instances
func (ic *InvidiousClient) GetVideoInfo(videoID string) (*InvidiousVideo, error) {
	var lastErr error

	for _, instance := range ic.Instances {
		apiURL := fmt.Sprintf("%s/api/v1/videos/%s", instance, videoID)
		fmt.Printf("Trying to get video info from Invidious instance: %s\n", instance)

		resp, err := ic.httpClient.Get(apiURL)
		if err != nil {
			lastErr = err
			fmt.Printf("Error accessing Invidious instance %s: %v\n", instance, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("Invidious instance %s returned status code %d", instance, resp.StatusCode)
			fmt.Println(lastErr)
			continue
		}

		var video InvidiousVideo
		if err := json.NewDecoder(resp.Body).Decode(&video); err != nil {
			lastErr = err
			fmt.Printf("Error decoding response from %s: %v\n", instance, err)
			continue
		}

		fmt.Printf("Successfully retrieved video info from %s: %s\n", instance, video.Title)
		return &video, nil
	}

	return nil, fmt.Errorf("failed to get video info from any Invidious instance: %v", lastErr)
}

// SearchVideos searches for videos using Invidious instances
func (ic *InvidiousClient) SearchVideos(query string, limit int) ([]InvidiousSearchResult, error) {
	var lastErr error

	for _, instance := range ic.Instances {
		// Encode the query for URL
		encodedQuery := url.QueryEscape(query)
		apiURL := fmt.Sprintf("%s/api/v1/search?q=%s&type=video&limit=%d", instance, encodedQuery, limit)
		fmt.Printf("Searching on Invidious instance: %s\n", instance)

		resp, err := ic.httpClient.Get(apiURL)
		if err != nil {
			lastErr = err
			fmt.Printf("Error accessing Invidious instance %s: %v\n", instance, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("Invidious instance %s returned status code %d", instance, resp.StatusCode)
			fmt.Println(lastErr)
			continue
		}

		var results []InvidiousSearchResult
		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			lastErr = err
			fmt.Printf("Error decoding response from %s: %v\n", instance, err)
			continue
		}

		// Filter out non-video results
		videoResults := make([]InvidiousSearchResult, 0)
		for _, result := range results {
			if result.Type == "video" {
				videoResults = append(videoResults, result)
			}
		}

		fmt.Printf("Found %d videos on %s\n", len(videoResults), instance)
		return videoResults, nil
	}

	return nil, fmt.Errorf("failed to search on any Invidious instance: %v", lastErr)
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

// GetInvidiousWatchURL returns a watch URL for a video on an Invidious instance
func (ic *InvidiousClient) GetInvidiousWatchURL(videoID string) string {
	// Use the first instance by default
	if len(ic.Instances) > 0 {
		return fmt.Sprintf("%s/watch?v=%s", ic.Instances[0], videoID)
	}

	// Fallback to yewtu.be if no instances are available
	return fmt.Sprintf("https://yewtu.be/watch?v=%s", videoID)
}

// IsInvidiousURL checks if a URL is from an Invidious instance
func (ic *InvidiousClient) IsInvidiousURL(urlStr string) bool {
	for _, instance := range ic.Instances {
		if strings.HasPrefix(urlStr, instance) {
			return true
		}
	}
	return false
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
