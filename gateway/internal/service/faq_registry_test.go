package service

import "testing"

func TestBuiltinFAQRegistryContainsExpectedEntries(t *testing.T) {
	t.Parallel()

	registry := NewBuiltinFAQRegistry()

	expected := []string{
		"faq.greeting.hello",
		"faq.identity.who_are_you",
		"faq.capability.what_can_you_do",
		"faq.platform.what_is_this",
	}

	for _, key := range expected {
		entry, ok := registry.Get(key)
		if !ok {
			t.Fatalf("expected faq entry %q to exist", key)
		}
		if entry.Key != key {
			t.Fatalf("expected faq key %q, got %q", key, entry.Key)
		}
		if entry.Title == "" || entry.Answer == "" || entry.Version == "" {
			t.Fatalf("expected populated faq entry, got %+v", entry)
		}
		if !entry.Enabled {
			t.Fatalf("expected faq entry %q to be enabled", key)
		}
		if len(entry.Tags) == 0 {
			t.Fatalf("expected faq entry %q to have tags", key)
		}
	}
}

func TestBuiltinFAQRegistryListReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	registry := NewBuiltinFAQRegistry()
	list := registry.List()
	if len(list) == 0 {
		t.Fatal("expected faq registry list to be non-empty")
	}

	list[0].Title = "mutated"
	list[0].Tags[0] = "mutated"

	entry, ok := registry.Get(list[0].Key)
	if !ok {
		t.Fatalf("expected faq entry %q to still exist", list[0].Key)
	}
	if entry.Title == "mutated" || entry.Tags[0] == "mutated" {
		t.Fatalf("expected defensive copy, got %+v", entry)
	}
}
