package youtube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kkdai/youtube/v2"
	"golang.org/x/net/proxy"
)

// Client handles YouTube audio downloads and streaming
type Client struct {
	YoutubeClient youtube.Client
	CacheDir      string
	httpClient    *http.Client
	proxyList     []string
	lastUpdate    time.Time
	proxyMutex    sync.Mutex
	currentProxy  int // Track current proxy index
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

	client := &Client{
		YoutubeClient: youtubeClient,
		CacheDir:      cacheDir,
		httpClient:    httpClient,
	}

	// Initialize proxy list
	go client.updateProxyList()
	go client.startProxyUpdater()

	return client
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

// updateProxyList fetches and updates the list of HTTP proxies
func (c *Client) updateProxyList() error {
	c.proxyMutex.Lock()
	defer c.proxyMutex.Unlock()

	fmt.Println("🔄 Fetching new proxy list...")
	resp, err := http.Get("https://raw.githubusercontent.com/Vann-Dev/proxy-list/refs/heads/main/proxies/http-tested/youtube.txt")
	if err != nil {
		fmt.Printf("❌ Error fetching proxy list: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Error reading proxy list: %v\n", err)
		return err
	}

	// Split the content by spaces to get individual proxy URLs
	rawProxies := strings.Fields(string(body))
	fmt.Printf("📥 Found %d raw proxies\n", len(rawProxies))

	// Create a channel to receive working proxies
	workingProxyChan := make(chan string)
	done := make(chan bool)
	var workingProxies []string

	// Start testing proxies in parallel
	for _, proxy := range rawProxies {
		go func(p string) {
			if c.testProxy(p) {
				select {
				case workingProxyChan <- p:
					// Proxy sent successfully
				case <-done:
					// Testing is done
				}
			}
		}(proxy)
	}

	// Collect working proxies for 10 seconds
	go func() {
		for proxy := range workingProxyChan {
			workingProxies = append(workingProxies, proxy)
			fmt.Printf("📝 Added working proxy to list: %s\n", proxy)
		}
	}()

	// Wait for timeout
	<-time.After(10 * time.Second)
	close(done)
	close(workingProxyChan)

	// Update the proxy list
	c.proxyList = workingProxies
	c.lastUpdate = time.Now()
	fmt.Printf("✅ Proxy list updated with %d working proxies\n", len(workingProxies))
	if len(workingProxies) > 0 {
		fmt.Println("📋 Working proxies:")
		for _, proxy := range workingProxies {
			fmt.Printf("  - %s\n", proxy)
		}
	}
	return nil
}

// testProxy tests if a proxy can access YouTube
func (c *Client) testProxy(proxyURL string) bool {
	fmt.Printf("Testing proxy: %s\n", proxyURL)

	// Create a test client with the proxy
	client := createProxyEnabledClientWithProxy(proxyURL)

	// Set a timeout for the test
	client.Timeout = 5 * time.Second

	// Try to access YouTube's API
	resp, err := client.Get("https://www.youtube.com/oembed?url=https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		fmt.Printf("❌ Proxy %s failed: %v\n", proxyURL, err)
		return false
	}
	defer resp.Body.Close()

	// Check if we got a successful response
	if resp.StatusCode == http.StatusOK {
		fmt.Printf("✅ Proxy %s is working!\n", proxyURL)
		return true
	}

	fmt.Printf("❌ Proxy %s failed with status code: %d\n", proxyURL, resp.StatusCode)
	return false
}

// startProxyUpdater starts a goroutine to update the proxy list every 20 minutes
func (c *Client) startProxyUpdater() {
	ticker := time.NewTicker(20 * time.Minute)
	for range ticker.C {
		fmt.Println("Updating proxy list...")
		c.updateProxyList()
	}
}

// getNextProxy returns the next proxy from the list
func (c *Client) getNextProxy() string {
	c.proxyMutex.Lock()
	defer c.proxyMutex.Unlock()

	if len(c.proxyList) == 0 {
		return ""
	}

	// Get the current proxy and increment the index
	proxy := c.proxyList[c.currentProxy]
	c.currentProxy = (c.currentProxy + 1) % len(c.proxyList)
	return proxy
}

// convertToDiscordFormat converts audio to a format that Discord can play
func (c *Client) convertToDiscordFormat(inFile, outFile string) error {
	fmt.Printf("Converting %s to %s\n", inFile, outFile)

	cmd := exec.Command("ffmpeg",
		"-i", inFile,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		outFile)

	// Capture ffmpeg output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		fmt.Printf("ffmpeg stdout:\n%s\n", stdout.String())
		fmt.Printf("ffmpeg stderr:\n%s\n", stderr.String())
		fmt.Printf("ffmpeg error: %v\n", err)
		return fmt.Errorf("ffmpeg conversion failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	// Log successful output
	if stdout.Len() > 0 {
		fmt.Printf("ffmpeg output:\n%s\n", stdout.String())
	}
	if stderr.Len() > 0 {
		fmt.Printf("ffmpeg warnings:\n%s\n", stderr.String())
	}

	return nil
}

// GetVideoInfo gets information about a YouTube video using the YouTube API with proxy support
func (c *Client) GetVideoInfo(videoID string) (string, error) {
	var lastError error
	// Try all proxies in the list
	for i := 0; i < len(c.proxyList); i++ {
		// Get next proxy from the list
		proxy := c.getNextProxy()
		if proxy != "" {
			fmt.Printf("Using proxy: %s\n", proxy)
			// Create a new client with the proxy
			proxyClient := createProxyEnabledClientWithProxy(proxy)
			c.YoutubeClient.HTTPClient = proxyClient
		}

		video, err := c.YoutubeClient.GetVideo(videoID)
		if err == nil {
			return video.Title, nil
		}

		lastError = err
		// If it's an age restriction error, try with the next proxy
		if strings.Contains(err.Error(), "login required to confirm your age") {
			fmt.Printf("Age restriction encountered, trying next proxy...\n")
			continue
		}
		// For other errors, return immediately
		return "", fmt.Errorf("failed to get video info: %v", err)
	}

	return "", fmt.Errorf("failed to get video info after trying all proxies: %v", lastError)
}

// SearchDeezer searches for a track on Deezer
func (c *Client) SearchDeezer(query string) (string, error) {
	// Deezer API endpoint for search
	url := fmt.Sprintf("https://api.deezer.com/search?q=%s", url.QueryEscape(query))

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to search Deezer: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
			Link  string `json:"link"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode Deezer response: %v", err)
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("no results found on Deezer")
	}

	// Return the first result's link
	return result.Data[0].Link, nil
}

// DownloadAudio downloads audio from Deezer using the YouTube video title
func (c *Client) DownloadAudio(videoID string) (string, error) {
	fmt.Printf("Attempting to download video: %s\n", videoID)

	// First try to get video info from YouTube
	title, err := c.GetVideoInfo(videoID)
	if err != nil {
		return "", fmt.Errorf("failed to get video info: %v", err)
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Create temporary files
	tmpFile := filepath.Join(c.CacheDir, videoID+".tmp")
	cachePath := filepath.Join(c.CacheDir, videoID+".pcm")
	fmt.Printf("Temporary files:\n- Input: %s\n- Output: %s\n", tmpFile, cachePath)

	// Search Deezer using video title
	fmt.Printf("Searching Deezer for: %s\n", title)
	deezerLink, err := c.SearchDeezer(title)
	if err != nil {
		return "", fmt.Errorf("failed to find track on Deezer: %v", err)
	}

	fmt.Printf("Found track on Deezer: %s\n", deezerLink)

	// Download from Deezer
	args := []string{
		"-f", "bestaudio",
		"-x", "--audio-format", "mp3",
		"-o", tmpFile,
		"--verbose",
		deezerLink,
	}

	cmd := exec.Command("yt-dlp", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to download from Deezer: %v", err)
	}

	// Success! Convert to Discord format
	fmt.Println("✅ Download successful, converting to Discord format...")
	err = c.convertToDiscordFormat(tmpFile, cachePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %v", err)
	}

	// Clean up the temporary file
	os.Remove(tmpFile)
	return cachePath, nil
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

// createProxyEnabledClientWithProxy creates an HTTP client with a specific proxy
func createProxyEnabledClientWithProxy(proxyURL string) *http.Client {
	// Start with the default transport
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Parse the proxy URL
	parsedURL, err := url.Parse("http://" + proxyURL) // Add http:// prefix if not present
	if err != nil {
		fmt.Printf("Error parsing proxy URL: %v\n", err)
		return &http.Client{Transport: transport}
	}

	// Set the proxy
	transport.Proxy = http.ProxyURL(parsedURL)
	fmt.Printf("Using HTTP proxy: %s\n", proxyURL)

	return &http.Client{
		Transport: transport,
	}
}
