package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrFAQCacheMiss = errors.New("service: faq cache miss")

type FAQCacheEntry struct {
	FAQKey    string    `json:"faq_key"`
	Answer    string    `json:"answer"`
	Version   string    `json:"version"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HitCount  int64     `json:"hit_count"`
}

type FAQCacheClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

type FAQCacheService interface {
	Get(ctx context.Context, platformAPIKeyID string, faq FAQEntry) (FAQCacheEntry, bool, error)
	Set(ctx context.Context, platformAPIKeyID string, faq FAQEntry) (FAQCacheEntry, error)
}

type noopFAQCacheService struct{}

type redisFAQCacheService struct {
	client FAQCacheClient
	ttl    time.Duration
	now    func() time.Time
}

func NewNoopFAQCacheService() FAQCacheService {
	return noopFAQCacheService{}
}

func NewFAQCacheService(client FAQCacheClient, ttl time.Duration) FAQCacheService {
	if client == nil {
		return noopFAQCacheService{}
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return redisFAQCacheService{
		client: client,
		ttl:    ttl,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (noopFAQCacheService) Get(context.Context, string, FAQEntry) (FAQCacheEntry, bool, error) {
	return FAQCacheEntry{}, false, nil
}

func (noopFAQCacheService) Set(_ context.Context, _ string, faq FAQEntry) (FAQCacheEntry, error) {
	now := time.Now().UTC()
	return FAQCacheEntry{
		FAQKey:    faq.Key,
		Answer:    faq.Answer,
		Version:   faq.Version,
		Source:    "builtin",
		CreatedAt: now,
		UpdatedAt: now,
		HitCount:  0,
	}, nil
}

func (s redisFAQCacheService) Get(ctx context.Context, platformAPIKeyID string, faq FAQEntry) (FAQCacheEntry, bool, error) {
	key, err := buildFAQCacheKey(platformAPIKeyID, faq.Key, faq.Version)
	if err != nil {
		return FAQCacheEntry{}, false, err
	}
	raw, err := s.client.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrFAQCacheMiss) {
			return FAQCacheEntry{}, false, nil
		}
		return FAQCacheEntry{}, false, err
	}
	var entry FAQCacheEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return FAQCacheEntry{}, false, err
	}
	entry.FAQKey = strings.TrimSpace(entry.FAQKey)
	entry.Answer = strings.TrimSpace(entry.Answer)
	entry.Version = strings.TrimSpace(entry.Version)
	entry.Source = strings.TrimSpace(entry.Source)
	return entry, true, nil
}

func (s redisFAQCacheService) Set(ctx context.Context, platformAPIKeyID string, faq FAQEntry) (FAQCacheEntry, error) {
	key, err := buildFAQCacheKey(platformAPIKeyID, faq.Key, faq.Version)
	if err != nil {
		return FAQCacheEntry{}, err
	}
	now := s.now()
	entry := FAQCacheEntry{
		FAQKey:    faq.Key,
		Answer:    faq.Answer,
		Version:   faq.Version,
		Source:    "builtin",
		CreatedAt: now,
		UpdatedAt: now,
		HitCount:  0,
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return FAQCacheEntry{}, err
	}
	if err := s.client.Set(ctx, key, string(payload), s.ttl); err != nil {
		return FAQCacheEntry{}, err
	}
	return entry, nil
}

func buildFAQCacheKey(platformAPIKeyID string, faqKey string, version string) (string, error) {
	apiKeyID := strings.TrimSpace(platformAPIKeyID)
	faqKey = strings.TrimSpace(faqKey)
	version = strings.TrimSpace(version)
	if apiKeyID == "" || faqKey == "" || version == "" {
		return "", fmt.Errorf("service: faq cache key requires platform_api_key_id, faq_key and version")
	}
	return fmt.Sprintf("faq_cache:%s:%s:%s", apiKeyID, faqKey, version), nil
}

type goRedisFAQCacheClient struct {
	client *redis.Client
}

func NewGoRedisFAQCacheClient(client *redis.Client) FAQCacheClient {
	if client == nil {
		return nil
	}
	return goRedisFAQCacheClient{client: client}
}

func (c goRedisFAQCacheClient) Get(ctx context.Context, key string) (string, error) {
	value, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrFAQCacheMiss
	}
	return value, err
}

func (c goRedisFAQCacheClient) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}
