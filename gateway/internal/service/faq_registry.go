package service

import "strings"

type FAQEntry struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Answer  string   `json:"answer"`
	Version string   `json:"version"`
	Enabled bool     `json:"enabled"`
	Tags    []string `json:"tags"`
}

type FAQRegistry interface {
	Get(key string) (FAQEntry, bool)
	List() []FAQEntry
}

type builtinFAQRegistry struct {
	order   []string
	entries map[string]FAQEntry
}

func NewBuiltinFAQRegistry() FAQRegistry {
	builtin := []FAQEntry{
		{
			Key:     "faq.greeting.hello",
			Title:   "问候语",
			Answer:  "你好！我是企业 AI Gateway 的智能助手，有什么可以帮你？",
			Version: "v1",
			Enabled: true,
			Tags:    []string{"greeting", "hello", "hi", "问候"},
		},
		{
			Key:     "faq.identity.who_are_you",
			Title:   "身份说明",
			Answer:  "我是企业 AI Gateway 提供的智能助手，用于统一接入和管理大模型能力。",
			Version: "v1",
			Enabled: true,
			Tags:    []string{"identity", "who_are_you", "你是谁"},
		},
		{
			Key:     "faq.capability.what_can_you_do",
			Title:   "能力说明",
			Answer:  "我可以回答平台常见问题，并把更复杂的请求交给后端大模型继续处理。",
			Version: "v1",
			Enabled: true,
			Tags:    []string{"capability", "what_can_you_do", "你可以做什么"},
		},
		{
			Key:     "faq.platform.what_is_this",
			Title:   "平台说明",
			Answer:  "这是企业 AI Gateway，用于统一接入、路由、治理和观测多种大模型能力。",
			Version: "v1",
			Enabled: true,
			Tags:    []string{"platform", "what_is_this", "这个平台是做什么的"},
		},
	}

	registry := builtinFAQRegistry{
		order:   make([]string, 0, len(builtin)),
		entries: make(map[string]FAQEntry, len(builtin)),
	}
	for _, entry := range builtin {
		registry.order = append(registry.order, entry.Key)
		registry.entries[entry.Key] = cloneFAQEntry(entry)
	}
	return registry
}

func (r builtinFAQRegistry) Get(key string) (FAQEntry, bool) {
	entry, ok := r.entries[strings.TrimSpace(key)]
	if !ok || !entry.Enabled {
		return FAQEntry{}, false
	}
	return cloneFAQEntry(entry), true
}

func (r builtinFAQRegistry) List() []FAQEntry {
	list := make([]FAQEntry, 0, len(r.order))
	for _, key := range r.order {
		entry, ok := r.entries[key]
		if !ok || !entry.Enabled {
			continue
		}
		list = append(list, cloneFAQEntry(entry))
	}
	return list
}

func cloneFAQEntry(entry FAQEntry) FAQEntry {
	entry.Tags = append([]string(nil), entry.Tags...)
	return entry
}
