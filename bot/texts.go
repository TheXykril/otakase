package main

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Texts staff can change on the dashboard without a new version: the
// welcome DM, the support post greeting and the FAQ answers. A change is
// kept in the data file; an empty change goes back to the built-in text.

type editableText struct {
	Key, Label string
	Default    func() string
}

func editableTexts() []editableText {
	list := []editableText{
		{"welcome_dm", "Welcome DM: greeting", func() string { return welcomeDMText }},
		{"welcome_steps", "Welcome DM: first steps (one per line)", func() string { return strings.Join(welcomeDMSteps, "\n") }},
		{"support_checklist", "Support post greeting", func() string { return supportChecklist }},
	}
	for _, f := range faqs {
		list = append(list, editableText{"faq:" + f.Key, "FAQ: " + f.Question, func() string { return f.Answer }})
	}
	return list
}

func findText(key string) (editableText, bool) {
	for _, t := range editableTexts() {
		if t.Key == key {
			return t, true
		}
	}
	return editableText{}, false
}

// text is a text as staff left it, else the built-in one.
func (b *bot) text(key string) string {
	var v string
	if b.store != nil {
		b.store.view(func(d *storeData) { v = d.Texts[key] })
	}
	if strings.TrimSpace(v) != "" {
		return v
	}
	if t, ok := findText(key); ok {
		return t.Default()
	}
	return ""
}

// faqAnswer is a FAQ entry with its answer as staff left it.
func (b *bot) faqAnswer(f faqEntry) faqEntry {
	f.Answer = b.text("faq:" + f.Key)
	return f
}

// welcomeDM is the welcome DM with the current texts.
func (b *bot) welcomeDM() *discordgo.MessageSend {
	var steps []string
	for _, l := range strings.Split(b.text("welcome_steps"), "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- ")); l != "" {
			steps = append(steps, l)
		}
	}
	return welcomeDMMessage(b.text("welcome_dm"), steps)
}
