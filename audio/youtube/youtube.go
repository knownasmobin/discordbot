package youtube

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/kkdai/youtube/v2"
	"golang.org/x/net/proxy"
)

// Client handles YouTube audio downloads and streaming
type Client struct {
	YoutubeClient youtube.Client
	CacheDir      string
	httpClient    *http.Client
}

// NewClient creates a new YouTube client
func NewClient() *Client {
	cacheDir := "./cache"
	// Create cache directory if it doesn't exist
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.Mkdir(cacheDir, 0755)
	}

	// Create an HTTP client with proxy support
	httpClient := createProxyEnabledClient()

	// Create a YouTube client with the proxy-enabled HTTP client
	youtubeClient := youtube.Client{
		HTTPClient: httpClient,
	}

	return &Client{
		YoutubeClient: youtubeClient,
		CacheDir:      cacheDir,
		httpClient:    httpClient,
	}
}

// createProxyEnabledClient creates an HTTP client with proxy support
func createProxyEnabledClient() *http.Client {
	// Start with the default transport
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Check for HTTP or HTTPS proxy
	httpProxy := os.Getenv("HTTP_PROXY")
	httpsProxy := os.Getenv("HTTPS_PROXY")
	socksProxy := os.Getenv("SOCKS_PROXY")
	proxyUser := os.Getenv("PROXY_USER")
	proxyPass := os.Getenv("PROXY_PASS")

	// Configure HTTP/HTTPS proxy if provided
	if httpProxy != "" || httpsProxy != "" {
		proxyURL := httpProxy
		if httpsProxy != "" {
			proxyURL = httpsProxy
		}

		if proxyURL != "" {
			// Add authentication if provided
			if proxyUser != "" && proxyPass != "" {
				proxyURL = strings.Replace(proxyURL, "://", fmt.Sprintf("://%s:%s@", proxyUser, proxyPass), 1)
			}

			if parsedURL, err := url.Parse(proxyURL); err == nil {
				transport.Proxy = http.ProxyURL(parsedURL)
				fmt.Printf("Using HTTP/HTTPS proxy: %s\n", proxyURL)
			} else {
				fmt.Printf("Error parsing proxy URL: %v\n", err)
			}
		}
	}

	// Configure SOCKS proxy if provided
	if socksProxy != "" {
		// Add authentication if provided
		if proxyUser != "" && proxyPass != "" {
			socksProxy = strings.Replace(socksProxy, "://", fmt.Sprintf("://%s:%s@", proxyUser, proxyPass), 1)
		}

		// Parse the SOCKS proxy URL
		socksURL, err := url.Parse(socksProxy)
		if err == nil {
			// Create a dialer that uses the SOCKS proxy
			dialer, err := proxy.FromURL(socksURL, proxy.Direct)
			if err == nil {
				// Override the dial function to use the SOCKS dialer
				transport.DialContext = dialer.(proxy.ContextDialer).DialContext
				fmt.Printf("Using SOCKS proxy: %s\n", socksProxy)
			} else {
				fmt.Printf("Error creating SOCKS dialer: %v\n", err)
			}
		} else {
			fmt.Printf("Error parsing SOCKS proxy URL: %v\n", err)
		}
	}

	// Return HTTP client with the configured transport
	return &http.Client{
		Transport: transport,
	}
}

// GetVideoID extracts the video ID from a YouTube URL
func (c *Client) GetVideoID(url string) (string, error) {
	// Regular expressions to match YouTube URL patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/watch\?v=([^&]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/embed/([^?]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/v/([^?]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtu\.be/([^?]+)`),
	}

	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(url); len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("invalid YouTube URL: %s", url)
}

// DownloadAudio downloads audio from a YouTube video
func (c *Client) DownloadAudio(videoID string) (string, error) {
	// First try with YouTube client
	video, err := c.YoutubeClient.GetVideo(videoID)
	if err != nil {
		// If YouTube fails, try with yt-dlp
		return c.downloadWithYtDlp(videoID)
	}

	// Get all available formats
	formats := video.Formats.WithAudioChannels()
	if len(formats) == 0 {
		return "", fmt.Errorf("no audio formats available")
	}

	// Try different formats in order of preference
	var stream io.ReadCloser
	for _, format := range formats {
		stream, _, err = c.YoutubeClient.GetStream(video, &format)
		if err == nil {
			break
		}
	}

	if stream == nil {
		// If all formats fail, try with yt-dlp
		return c.downloadWithYtDlp(videoID)
	}
	defer stream.Close()

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Create a temporary file for the downloaded audio
	tmpFile := filepath.Join(c.CacheDir, videoID+".tmp")
	outFile, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer outFile.Close()

	// Copy the stream to the file
	_, err = io.Copy(outFile, stream)
	if err != nil {
		return "", fmt.Errorf("failed to download audio: %v", err)
	}

	// Convert to Discord format
	cachePath := filepath.Join(c.CacheDir, videoID+".pcm")
	err = c.convertToDiscordFormat(tmpFile, cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %v", err)
	}

	// Clean up the temporary file
	os.Remove(tmpFile)

	return cachePath, nil
}

// downloadWithYtDlp downloads audio using yt-dlp
func (c *Client) downloadWithYtDlp(videoID string) (string, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Create temporary files
	tmpFile := filepath.Join(c.CacheDir, videoID+".tmp")
	cachePath := filepath.Join(c.CacheDir, videoID+".pcm")

	// Download audio using yt-dlp
	cmd := exec.Command("yt-dlp",
		"-f", "bestaudio",
		"-x", "--audio-format", "mp3",
		"-o", tmpFile,
		"https://www.youtube.com/watch?v="+videoID)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to download with yt-dlp: %v", err)
	}

	// Convert to Discord format
	err := c.convertToDiscordFormat(tmpFile, cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %v", err)
	}

	// Clean up the temporary file
	os.Remove(tmpFile)

	return cachePath, nil
}

// convertToDiscordFormat converts audio to a format that Discord can play
func (c *Client) convertToDiscordFormat(inFile, outFile string) error {
	cmd := exec.Command("ffmpeg",
		"-i", inFile,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		outFile)

	return cmd.Run()
}

// Play plays YouTube audio in a Discord voice channel
func (c *Client) Play(vc *discordgo.VoiceConnection, url string) error {
	// Extract video ID
	videoID, err := c.GetVideoID(url)
	if err != nil {
		return err
	}

	// Try to download audio with retries
	var audioFile string
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		audioFile, err = c.DownloadAudio(videoID)
		if err == nil {
			break
		}

		// If it's an age restriction error, try with a different proxy
		if strings.Contains(err.Error(), "login required to confirm your age") {
			// Rotate proxy configuration
			c.httpClient = createProxyEnabledClient()
			c.YoutubeClient.HTTPClient = c.httpClient
			continue
		}

		// For other errors, return immediately
		return err
	}

	if err != nil {
		return fmt.Errorf("failed to download audio after %d retries: %v", maxRetries, err)
	}

	// Start speaking
	vc.Speaking(true)
	defer vc.Speaking(false)

	// Read the file
	file, err := os.Open(audioFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a buffer for sending
	buf := make([]byte, 16*1024)
	for {
		n, err := file.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// Send the buffer
		vc.OpusSend <- buf[:n]
	}

	return nil
}
