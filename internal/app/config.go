package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port              string
	DataDir           string
	DatabasePath      string
	EncryptionKey     []byte
	AdminUsername     string
	AdminPassword     string
	SecureCookies     bool
	BilibiliBaseURL   string
	DefaultBarkServer string
}

func LoadConfig() (Config, error) {
	dataDir := envOr("UP_UPDATE_DATA_DIR", "./data")
	keyText := strings.TrimSpace(os.Getenv("UP_UPDATE_ENCRYPTION_KEY"))
	if keyFile := strings.TrimSpace(os.Getenv("UP_UPDATE_ENCRYPTION_KEY_FILE")); keyText == "" && keyFile != "" {
		value, err := os.ReadFile(keyFile)
		if err != nil {
			return Config{}, err
		}
		keyText = strings.TrimSpace(string(value))
	}
	key, err := parseEncryptionKey(keyText)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Port:              envOr("UP_UPDATE_PORT", "8080"),
		DataDir:           dataDir,
		DatabasePath:      filepath.Join(dataDir, "up-update.db"),
		EncryptionKey:     key,
		AdminUsername:     envOr("UP_UPDATE_ADMIN_USERNAME", "admin"),
		AdminPassword:     os.Getenv("UP_UPDATE_ADMIN_PASSWORD"),
		SecureCookies:     strings.EqualFold(os.Getenv("UP_UPDATE_SECURE_COOKIES"), "true"),
		BilibiliBaseURL:   envOr("UP_UPDATE_BILIBILI_BASE_URL", "https://api.bilibili.com"),
		DefaultBarkServer: envOr("UP_UPDATE_BARK_SERVER", "https://api.day.app"),
	}, nil
}

func parseEncryptionKey(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("UP_UPDATE_ENCRYPTION_KEY or UP_UPDATE_ENCRYPTION_KEY_FILE is required")
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(value) >= 32 {
		sum := sha256.Sum256([]byte(value))
		return sum[:], nil
	}
	return nil, errors.New("encryption key must be 32-byte hex/base64 or at least 32 characters")
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
