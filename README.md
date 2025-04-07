# Discord Music Bot in Go

A Discord bot that can play music from YouTube and Spotify using FFmpeg with slash commands.

## Prerequisites

- [Go](https://golang.org/) (1.16 or later)
- [FFmpeg](https://ffmpeg.org/) (must be installed and available in PATH)
- Discord Bot Token
- (Optional) Spotify Developer credentials
- (Optional) Proxy server for YouTube access

## Installation

1. Clone this repository
2. Install the dependencies:
   ```
   go mod download
   ```
3. Create a `.env` file in the root directory with the following content:
   ```
   DISCORD_TOKEN=your_discord_bot_token_here
   SPOTIFY_ID=your_spotify_client_id_here (optional)
   SPOTIFY_SECRET=your_spotify_client_secret_here (optional)
   
   # Proxy Configuration (optional)
   # HTTP_PROXY=http://proxy.example.com:8080
   # HTTPS_PROXY=https://proxy.example.com:8080
   # SOCKS_PROXY=socks5://proxy.example.com:1080
   ```
4. Make sure your Discord bot has the following permissions:
   - `applications.commands` (to register slash commands)
   - `bot` (with Send Messages, Connect, and Speak permissions)

## Proxy Configuration

The bot supports proxying YouTube requests through HTTP, HTTPS, or SOCKS proxies. This can help bypass regional restrictions or network limitations:

- **HTTP Proxy**: Set `HTTP_PROXY=http://proxy.example.com:8080` in your `.env` file
- **HTTPS Proxy**: Set `HTTPS_PROXY=https://proxy.example.com:8080` in your `.env` file
- **SOCKS Proxy**: Set `SOCKS_PROXY=socks5://proxy.example.com:1080` in your `.env` file

For proxies requiring authentication, use the format: `http://username:password@proxy.example.com:8080`

## Usage

Run the bot with:
```
go run main.go
```

## Bot Commands

The bot uses slash commands for all operations:

- `/ping` - Test if the bot is responding
- `/join` - Make the bot join your voice channel
- `/leave` - Make the bot leave the voice channel
- `/play [url]` - Add a YouTube or Spotify URL to the queue and start playing if nothing is already playing
- `/queue` - Show the current queue
- `/queue [url]` - Add a YouTube or Spotify URL to the queue
- `/repeat` - Toggle repeat mode (current song will be added back to the queue after playing)
- `/autoplay` - Toggle autoplay mode (bot will continue playing similar tracks when the queue is empty)

Supported URL formats:
- YouTube: `https://www.youtube.com/watch?v=...` or `https://youtu.be/...`
- Spotify: `https://open.spotify.com/track/...`

## Notes

- The bot requires FFmpeg to be installed and available in your PATH for audio conversion
- YouTube playback is implemented directly
- Spotify playback is implemented by searching the track on YouTube
- A cache directory will be created to store downloaded audio files
- Repeat mode will add the current track back to the queue after it finishes
- Autoplay mode will find and add related videos to the queue when it's empty
  - For YouTube videos, finds related videos based on the current video
  - For Spotify tracks, finds similar tracks by the same artist

## Dependencies

- [discordgo](https://github.com/bwmarrin/discordgo) - Go binding for Discord API
- [youtube](https://github.com/kkdai/youtube) - YouTube video downloader
- [spotify](https://github.com/zmb3/spotify) - Go wrapper for Spotify Web API
- [godotenv](https://github.com/joho/godotenv) - .env file loader 