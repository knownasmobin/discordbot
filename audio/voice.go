package audio

import (
	"errors"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// VoiceInstance represents a voice connection to a Discord guild
type VoiceInstance struct {
	GuildID    string
	ChannelID  string
	Connection *discordgo.VoiceConnection
	IsPlaying  bool
	Repeat     bool
	Autoplay   bool
	CurrentURL string
	Queue      []string
	Mu         sync.Mutex
}

// VoiceManager manages voice connections
type VoiceManager struct {
	Instances map[string]*VoiceInstance
	Mu        sync.Mutex
}

// NewVoiceManager creates a new voice manager
func NewVoiceManager() *VoiceManager {
	return &VoiceManager{
		Instances: make(map[string]*VoiceInstance),
	}
}

// GetVoiceInstance gets or creates a voice instance for a guild
func (vm *VoiceManager) GetVoiceInstance(guildID string) *VoiceInstance {
	vm.Mu.Lock()
	defer vm.Mu.Unlock()

	if instance, exists := vm.Instances[guildID]; exists {
		return instance
	}

	instance := &VoiceInstance{
		GuildID: guildID,
	}
	vm.Instances[guildID] = instance
	return instance
}

// Join connects to a voice channel
func (vi *VoiceInstance) Join(s *discordgo.Session, channelID string) error {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()

	// If we're already connected to this channel, do nothing
	if vi.Connection != nil && vi.ChannelID == channelID {
		return nil
	}

	// Disconnect from current channel if we're in one
	if vi.Connection != nil {
		vi.Connection.Disconnect()
	}

	// Connect to the new channel
	vc, err := s.ChannelVoiceJoin(vi.GuildID, channelID, false, true)
	if err != nil {
		return err
	}

	vi.Connection = vc
	vi.ChannelID = channelID
	return nil
}

// Leave disconnects from a voice channel
func (vi *VoiceInstance) Leave() error {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()

	if vi.Connection == nil {
		return errors.New("not connected to a voice channel")
	}

	err := vi.Connection.Disconnect()
	if err != nil {
		return err
	}

	vi.Connection = nil
	vi.ChannelID = ""
	return nil
}

// AddToQueue adds a URL to the queue
func (vi *VoiceInstance) AddToQueue(url string) {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()
	vi.Queue = append(vi.Queue, url)
}

// GetNextFromQueue gets the next item from the queue
func (vi *VoiceInstance) GetNextFromQueue() (string, bool) {
	vi.Mu.Lock()
	defer vi.Mu.Unlock()

	if len(vi.Queue) == 0 {
		return "", false
	}

	url := vi.Queue[0]
	vi.Queue = vi.Queue[1:]
	vi.CurrentURL = url
	return url, true
}
