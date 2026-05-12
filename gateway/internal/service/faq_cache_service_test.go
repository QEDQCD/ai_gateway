package service

import (
	"context"
	"testing"
	"time"
)

func TestBuildFAQCacheKeyUsesAPIKeyAndVersion(t *testing.T) {
	t.Parallel()

	key, err := buildFAQCacheKey("pak_live_console", "faq.identity.who_are_you", "v1")
	if err != nil {
		t.Fatalf("buildFAQCacheKey returned error: %v", err)
	}
	want := "faq_cache:pak_live_console:faq.identity.who_are_you:v1"
	if key != want {
		t.Fatalf("expected %q, got %q", want, key)
	}
}

func TestFAQCacheServiceSetAndGetRoundTrip(t *testing.T) {
	t.Parallel()

	client := newFakeFAQCacheClient()
	cache := NewFAQCacheService(client, 5*time.Minute)
	ctx := context.WithValue(context.Background(), faqCacheTestContextKey{}, "faq-cache")
	faq := FAQEntry{
		Key:     "faq.platform.what_is_this",
		Title:   "平台说明",
		Answer:  "这是 AI Gateway。",
		Version: "v1",
		Enabled: true,
	}

	entry, err := cache.Set(ctx, "pak_live_console", faq)
	if err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if entry.Source != "builtin" {
		t.Fatalf("expected source builtin, got %+v", entry)
	}
	if client.lastSetTTL != 5*time.Minute {
		t.Fatalf("expected ttl 5m, got %v", client.lastSetTTL)
	}
	if client.lastSetKey != "faq_cache:pak_live_console:faq.platform.what_is_this:v1" {
		t.Fatalf("unexpected set key %q", client.lastSetKey)
	}

	got, ok, err := cache.Get(ctx, "pak_live_console", faq)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if got.FAQKey != faq.Key || got.Answer != faq.Answer || got.Version != faq.Version {
		t.Fatalf("unexpected cached entry %+v", got)
	}
}

func TestFAQCacheServiceGetReturnsMissWhenEntryAbsent(t *testing.T) {
	t.Parallel()

	cache := NewFAQCacheService(newFakeFAQCacheClient(), time.Minute)
	faq := FAQEntry{Key: "faq.identity.who_are_you", Version: "v1"}

	_, ok, err := cache.Get(context.Background(), "pak_live_console", faq)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if ok {
		t.Fatal("expected cache miss")
	}
}

type fakeFAQCacheClient struct {
	values map[string]string

	lastGetContext context.Context
	lastGetKey     string

	lastSetContext context.Context
	lastSetKey     string
	lastSetValue   string
	lastSetTTL     time.Duration
}

func newFakeFAQCacheClient() *fakeFAQCacheClient {
	return &fakeFAQCacheClient{values: make(map[string]string)}
}

func (f *fakeFAQCacheClient) Get(ctx context.Context, key string) (string, error) {
	f.lastGetContext = ctx
	f.lastGetKey = key
	value, ok := f.values[key]
	if !ok {
		return "", ErrFAQCacheMiss
	}
	return value, nil
}

func (f *fakeFAQCacheClient) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	f.lastSetContext = ctx
	f.lastSetKey = key
	f.lastSetValue = value
	f.lastSetTTL = ttl
	f.values[key] = value
	return nil
}

type faqCacheTestContextKey struct{}
