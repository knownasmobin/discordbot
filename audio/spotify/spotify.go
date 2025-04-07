package spotify

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"discordbot/audio/youtube"

	"github.com/bwmarrin/discordgo"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"
)

// Client handles Spotify integration
type Client struct {
	SpotifyClient *spotify.Client
	YouTubeSearch func(query string) (string, error)
}

// NewClient creates a new Spotify client
func NewClient(youtubeSearch func(query string) (string, error)) (*Client, error) {
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
		YouTubeSearch: youtubeSearch,
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

// PlayTrack plays a Spotify track via YouTube search
func (c *Client) PlayTrack(vc *discordgo.VoiceConnection, url string) error {
	// Extract track ID
	trackID, err := c.GetTrackID(url)
	if err != nil {
		return err
	}

	// Get track info
	track, err := c.GetTrackInfo(trackID)
	if err != nil {
		return err
	}

	// Construct search query for YouTube
	artists := make([]string, len(track.Artists))
	for i, artist := range track.Artists {
		artists[i] = artist.Name
	}

	query := fmt.Sprintf("%s - %s", strings.Join(artists, ", "), track.Name)

	// Search on YouTube and get URL
	youtubeURL, err := c.YouTubeSearch(query)
	if err != nil {
		return fmt.Errorf("failed to find track on YouTube: %v", err)
	}

	// Use the YouTube client to play the track
	ytClient := youtube.NewClient()
	return ytClient.Play(vc, youtubeURL)
}

// GetRelatedTrack finds a related track based on the current Spotify track
func (c *Client) GetRelatedTrack(url string) (string, error) {
	// Extract track ID
	trackID, err := c.GetTrackID(url)
	if err != nil {
		return "", err
	}

	// Get track info
	track, err := c.GetTrackInfo(trackID)
	if err != nil {
		return "", err
	}

	// Get artist name
	var artistName string
	if len(track.Artists) > 0 {
		artistName = track.Artists[0].Name
	} else {
		return "", fmt.Errorf("no artist found for track")
	}

	// Use YouTube client to find similar tracks by the same artist
	ytClient := youtube.NewClient()
	return ytClient.GetRelatedSpotifyTrack(artistName, track.Name)
}
