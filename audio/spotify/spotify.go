package spotify

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"

	"discordbot/audio/youtube"

	"github.com/bwmarrin/discordgo"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
)

// Client handles Spotify integration
type Client struct {
	SpotifyClient *spotify.Client
	YouTubeClient *youtube.Client
}

// NewClient creates a new Spotify client
func NewClient(ytClient *youtube.Client) (*Client, error) {
	// Get Spotify credentials from environment
	clientID := os.Getenv("SPOTIFY_ID")
	clientSecret := os.Getenv("SPOTIFY_SECRET")

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("SPOTIFY_ID and SPOTIFY_SECRET must be set in .env file")
	}

	// Set up the Spotify client
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     spotifyauth.TokenURL,
	}

	token, err := config.Token(context.Background())
	if err != nil {
		return nil, fmt.Errorf("couldn't get Spotify token: %v", err)
	}

	httpClient := spotifyauth.New().Client(context.Background(), token)
	client := spotify.New(httpClient)

	return &Client{
		SpotifyClient: client,
		YouTubeClient: ytClient,
	}, nil
}

// GetTrackID extracts the track ID from a Spotify URL
func (c *Client) GetTrackID(url string) (string, error) {
	// Regular expressions to match Spotify URL patterns
	trackRegex := regexp.MustCompile(`^https?://(?:open\.)?spotify\.com/track/([a-zA-Z0-9]+)`)

	if matches := trackRegex.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("invalid Spotify track URL: %s", url)
}

// GetPlaylistID extracts the playlist ID from a Spotify URL
func (c *Client) GetPlaylistID(url string) (string, error) {
	// Regular expressions to match Spotify URL patterns
	playlistRegex := regexp.MustCompile(`^https?://(?:open\.)?spotify\.com/playlist/([a-zA-Z0-9]+)`)

	if matches := playlistRegex.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("invalid Spotify playlist URL: %s", url)
}

// GetTrackInfo gets track information from Spotify
func (c *Client) GetTrackInfo(trackID string) (*spotify.FullTrack, error) {
	track, err := c.SpotifyClient.GetTrack(context.Background(), spotify.ID(trackID))
	if err != nil {
		return nil, fmt.Errorf("failed to get track info: %v", err)
	}
	return track, nil
}

// Search searches for a track on YouTube and returns the first result
func (c *Client) Search(query string) (string, error) {
	// Get track info from Spotify
	trackID, err := c.GetTrackID(query)
	if err != nil {
		return "", fmt.Errorf("invalid Spotify URL: %v", err)
	}

	track, err := c.GetTrackInfo(trackID)
	if err != nil {
		return "", fmt.Errorf("failed to get track info: %v", err)
	}

	// Create search query
	searchQuery := fmt.Sprintf("%s - %s", track.Name, track.Artists[0].Name)
	
	// For now, we'll just return a YouTube URL directly since we don't have a search implementation
	// In a real implementation, you would use the YouTube Data API or yt-dlp to search
	return fmt.Sprintf("https://www.youtube.com/results?search_query=%s", url.QueryEscape(searchQuery)), nil
}

// PlayTrack plays a Spotify track via YouTube search
func (c *Client) PlayTrack(vc *discordgo.VoiceConnection, url string) error {
	// Search for the track on YouTube
	youtubeURL, err := c.Search(url)
	if err != nil {
		return fmt.Errorf("failed to find track on YouTube: %v", err)
	}

	// Use the YouTube client to play the track
	return c.YouTubeClient.Play(vc, youtubeURL)
}

// GetRelatedTrack finds a related track based on the current Spotify track
func (c *Client) GetRelatedTrack(trackURL string) (string, error) {
	// Extract track ID
	trackID, err := c.GetTrackID(trackURL)
	if err != nil {
		return "", fmt.Errorf("invalid Spotify URL: %v", err)
	}

	// Get track info
	track, err := c.GetTrackInfo(trackID)
	if err != nil {
		return "", fmt.Errorf("failed to get track info: %v", err)
	}

	// Get artist name
	if len(track.Artists) == 0 {
		return "", fmt.Errorf("no artist found for track")
	}

	// Create a search query for related tracks
	searchQuery := fmt.Sprintf("%s %s official audio", track.Artists[0].Name, track.Name)
	
	// Return a YouTube search URL for the related track
	return fmt.Sprintf("https://www.youtube.com/results?search_query=%s", url.QueryEscape(searchQuery)), nil
}
