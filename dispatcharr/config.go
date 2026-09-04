package dispatcharr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const CurrentConfigVersion = 1

const (
	AuthPassword = "password"
	AuthAPIKey   = "api-key"
)

type Config struct {
	Version    int    `json:"version"`
	BaseURL    string `json:"baseUrl"`
	AuthMethod string `json:"authMethod,omitempty"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	APIKey     string `json:"apiKey,omitempty"`
}

func (c Config) Normalized() (Config, error) {
	c.Version = CurrentConfigVersion
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.AuthMethod = strings.ToLower(strings.TrimSpace(c.AuthMethod))
	c.Username = strings.TrimSpace(c.Username)
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.AuthMethod == "" {
		c.AuthMethod = AuthPassword
	}
	if c.BaseURL == "" {
		return Config{}, errors.New("Dispatcharr URL is required")
	}
	switch c.AuthMethod {
	case AuthPassword:
		if c.Username == "" || c.Password == "" {
			return Config{}, errors.New("Dispatcharr username and password are required")
		}
		c.APIKey = ""
	case AuthAPIKey:
		if c.APIKey == "" {
			return Config{}, errors.New("Dispatcharr API key is required")
		}
		c.Username = ""
		c.Password = ""
	default:
		return Config{}, errors.New("Dispatcharr authentication method must be password or API key")
	}
	if len(c.BaseURL) > 2048 || len(c.Username) > 256 || len(c.Password) > 4096 || len(c.APIKey) > 4096 {
		return Config{}, errors.New("Dispatcharr connection settings are too long")
	}
	parsed, err := url.Parse(c.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Config{}, errors.New("Dispatcharr URL must be a complete HTTP or HTTPS address")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Config{}, errors.New("Dispatcharr URL cannot contain credentials, a query, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	c.BaseURL = strings.TrimRight(parsed.String(), "/")
	return c, nil
}

func (c Config) Fingerprint() string {
	parsed, _ := url.Parse(c.BaseURL)
	origin := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
	identity := c.Username
	if c.AuthMethod == AuthAPIKey {
		identity = AuthAPIKey
	}
	sum := sha256.Sum256([]byte(origin + parsed.EscapedPath() + "\x00" + c.AuthMethod + "\x00" + identity))
	return hex.EncodeToString(sum[:])
}

type ConfigStore struct {
	mu         sync.RWMutex
	path       string
	config     Config
	configured bool
}

func LoadConfigStore(path string) (*ConfigStore, error) {
	store := &ConfigStore{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return store, fmt.Errorf("reading Dispatcharr configuration: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return store, fmt.Errorf("decoding Dispatcharr configuration: %w", err)
	}
	if config.Version != CurrentConfigVersion {
		return store, fmt.Errorf("unsupported Dispatcharr configuration version %d", config.Version)
	}
	config, err = config.Normalized()
	if err != nil {
		return store, fmt.Errorf("validating Dispatcharr configuration: %w", err)
	}
	store.config = config
	store.configured = true
	return store, nil
}

func (s *ConfigStore) Get() (Config, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config, s.configured
}

func (s *ConfigStore) Save(config Config) error {
	config, err := config.Normalized()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding Dispatcharr configuration: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writePrivateFile(s.path, data); err != nil {
		return err
	}
	s.config = config
	s.configured = true
	return nil
}

func (s *ConfigStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing Dispatcharr configuration: %w", err)
	}
	s.config = Config{}
	s.configured = false
	return nil
}

func (s *ConfigStore) WhileCurrent(fingerprint string, fn func() error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configured || s.config.Fingerprint() != fingerprint {
		return false, nil
	}
	return true, fn()
}

func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating Dispatcharr configuration directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".dispatcharr-config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary Dispatcharr configuration: %w", err)
	}
	name := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("securing temporary Dispatcharr configuration: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing Dispatcharr configuration: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing Dispatcharr configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary Dispatcharr configuration: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replacing Dispatcharr configuration: %w", err)
	}
	keep = true
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("securing Dispatcharr configuration: %w", err)
	}
	return nil
}
