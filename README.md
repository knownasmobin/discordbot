# Discord Music Bot

A Discord bot that plays music from YouTube in voice channels.

## Features

- Play music from YouTube
- Queue system for multiple songs
- Skip, pause, and resume functionality
- Volume control
- Age-restricted video support (requires cookie file)
- Automatic format conversion for Discord compatibility

## Prerequisites

- Go 1.16 or higher
- FFmpeg installed and in PATH
- yt-dlp installed and in PATH
- Discord Bot Token
- YouTube Cookie File (for age-restricted videos)

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/discordbot.git
cd discordbot
```

2. Install dependencies:
```bash
go mod download
```

3. Install FFmpeg and yt-dlp:
```bash
# Ubuntu/Debian
sudo apt-get install ffmpeg
pip install yt-dlp

# Windows
# Download FFmpeg from https://ffmpeg.org/download.html
# Install yt-dlp using pip: pip install yt-dlp
```

4. Create a `.env` file with your Discord bot token:
```bash
DISCORD_TOKEN=your_discord_bot_token
```

5. (Optional) Set up YouTube cookie file for age-restricted videos:
```bash
# Export cookies from your browser using an extension like "Get cookies.txt"
# Then set the environment variable:
export YT_COOKIE_FILE="/path/to/your/cookies.txt"

# Windows PowerShell
$env:YT_COOKIE_FILE = "C:\path\to\your\cookies.txt"
```

## Usage

1. Run the bot:
```bash
go run main.go
```

2. Commands:
- `!play <url>` - Play a YouTube video
- `!skip` - Skip the current song
- `!pause` - Pause playback
- `!resume` - Resume playback
- `!stop` - Stop playback and clear queue
- `!queue` - Show the current queue
- `!volume <1-100>` - Set volume level

## Troubleshooting
### Age-restricted Videos and IP Restrictions
If you encounter issues with age-restricted videos or IP restrictions:

#### Cookie-based Authentication (Recommended)
1. Make sure you have exported your YouTube cookies correctly
2. Verify the cookie file path in YT_COOKIE_FILE
3. Check that the cookie file is in Netscape format
4. Ensure you're logged into YouTube when exporting cookies

#### Enhanced YouTube Integration
The bot now uses the [goutubedl](https://github.com/wader/goutubedl) wrapper for improved YouTube video handling:
- Robust handling of age-restricted content
- Better error recovery and fallback mechanisms
- Simplified integration with yt-dlp
- Automatic proxy support

#### Alternative Methods (Automatic Fallbacks)
The bot includes several fallback methods that automatically activate when restrictions are encountered:
1. Enhanced yt-dlp parameters to bypass restrictions
2. Alternative YouTube frontends (Invidious instances)
3. Direct YouTube API access with randomized headers
4. Deezer music service as a last resort

#### Proxy Configuration
If you continue to experience IP restrictions, you can configure proxies in your .env file:
```
# HTTP/HTTPS proxy (standard web proxy)
HTTP_PROXY=http://proxy.example.com:8080
HTTPS_PROXY=https://proxy.example.com:8080

# SOCKS proxy (more anonymous)
SOCKS_PROXY=socks5://proxy.example.com:1080
```

### Common Errors
- "Sign in to confirm you're not a bot" - Check your cookie file setup
- "No audio formats available" - Video might be region-locked or private
- FFmpeg errors - Verify FFmpeg installation and PATH

## Contributing

Feel free to submit issues and pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.