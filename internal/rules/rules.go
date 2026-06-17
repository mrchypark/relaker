package rules

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

type Event struct {
	Source     string          `json:"source"`
	Receiver   string          `json:"receiver,omitempty"`
	Workspace  string          `json:"workspace,omitempty"`
	Event      string          `json:"event"`
	Action     string          `json:"action,omitempty"`
	ID         string          `json:"id,omitempty"`
	EnvelopeID string          `json:"envelope_id,omitempty"`
	Repo       string          `json:"repo,omitempty"`
	BaseRef    string          `json:"base_ref,omitempty"`
	Labels     []string        `json:"labels,omitempty"`
	Channel    string          `json:"channel,omitempty"`
	User       string          `json:"user,omitempty"`
	Text       string          `json:"text,omitempty"`
	Reaction   string          `json:"reaction,omitempty"`
	Payload    json.RawMessage `json:"-"`
}

func (e Event) DedupeKeys() []string {
	var keys []string
	source := e.sourceKey()
	if e.ID != "" {
		keys = append(keys, source+":"+e.ID)
	}
	if e.EnvelopeID != "" && e.EnvelopeID != e.ID {
		keys = append(keys, source+":envelope:"+e.EnvelopeID)
	}
	return keys
}

func (e Event) sourceKey() string {
	if e.Source == "github" && e.Receiver != "" {
		return e.Source + ":" + e.Receiver
	}
	if e.Source == "slack" && e.Workspace != "" {
		return e.Source + ":" + e.Workspace
	}
	return e.Source
}

type Rule struct {
	Source     string   `yaml:"source"`
	Receiver   string   `yaml:"receiver"`
	Workspace  string   `yaml:"workspace"`
	Event      string   `yaml:"event"`
	Actions    []string `yaml:"actions"`
	Repo       string   `yaml:"repo"`
	BaseRef    string   `yaml:"base_ref"`
	LabelsAll  []string `yaml:"labels_all"`
	LabelsAny  []string `yaml:"labels_any"`
	EventID    string   `yaml:"event_id"`
	EnvelopeID string   `yaml:"envelope_id"`
	Channel    string   `yaml:"channel"`
	User       string   `yaml:"user"`
	TextRegex  string   `yaml:"text_regex"`
	Reaction   string   `yaml:"reaction"`
	Run        string   `yaml:"run"`
}

type Set struct {
	rules []compiledRule
}

type compiledRule struct {
	Rule
	textRegex *regexp.Regexp
}

type Skip struct {
	Rule   Rule
	Reason string
}

func NewSet(ruleList []Rule) (*Set, error) {
	set := &Set{rules: make([]compiledRule, 0, len(ruleList))}
	for i, rule := range ruleList {
		if rule.Source == "" {
			return nil, fmt.Errorf("rule %d: source is required", i)
		}
		if rule.Source != "github" && rule.Source != "slack" {
			return nil, fmt.Errorf("rule %d: source %q is not supported", i, rule.Source)
		}
		if rule.Event == "" {
			return nil, fmt.Errorf("rule %d: event is required", i)
		}
		if rule.Run == "" {
			return nil, fmt.Errorf("rule %d: run is required", i)
		}
		compiled := compiledRule{Rule: rule}
		if rule.TextRegex != "" {
			re, err := regexp.Compile(rule.TextRegex)
			if err != nil {
				return nil, fmt.Errorf("rule %d: compile text_regex: %w", i, err)
			}
			compiled.textRegex = re
		}
		set.rules = append(set.rules, compiled)
	}
	return set, nil
}

func (s *Set) Match(event Event) ([]Rule, []Skip) {
	var matches []Rule
	var skips []Skip
	for _, rule := range s.rules {
		if reason := rule.skipReason(event); reason != "" {
			skips = append(skips, Skip{Rule: rule.Rule, Reason: reason})
			continue
		}
		matches = append(matches, rule.Rule)
	}
	return matches, skips
}

func (s *Set) Runs() []string {
	runs := make([]string, 0, len(s.rules))
	for _, rule := range s.rules {
		runs = append(runs, rule.Run)
	}
	return runs
}

func (r compiledRule) skipReason(event Event) string {
	if r.Source != event.Source {
		return "source mismatch"
	}
	if r.Receiver != "" && r.Receiver != event.Receiver {
		return "receiver mismatch"
	}
	if r.Workspace != "" && r.Workspace != event.Workspace {
		return "workspace mismatch"
	}
	if r.Event != event.Event {
		return "event mismatch"
	}
	if len(r.Actions) > 0 && !slices.Contains(r.Actions, event.Action) {
		return "action mismatch"
	}
	if r.Repo != "" && r.Repo != event.Repo {
		return "repo mismatch"
	}
	if r.BaseRef != "" && r.BaseRef != event.BaseRef {
		return "base_ref mismatch"
	}
	if len(r.LabelsAll) > 0 && !containsAll(event.Labels, r.LabelsAll) {
		return "labels_all mismatch"
	}
	if len(r.LabelsAny) > 0 && !containsAny(event.Labels, r.LabelsAny) {
		return "labels_any mismatch"
	}
	if r.EventID != "" && r.EventID != event.ID {
		return "event_id mismatch"
	}
	if r.EnvelopeID != "" && r.EnvelopeID != event.EnvelopeID {
		return "envelope_id mismatch"
	}
	if r.Channel != "" && r.Channel != event.Channel {
		return "channel mismatch"
	}
	if r.User != "" && r.User != event.User {
		return "user mismatch"
	}
	if r.Reaction != "" && r.Reaction != event.Reaction {
		return "reaction mismatch"
	}
	if r.textRegex != nil && !r.textRegex.MatchString(event.Text) {
		return "text_regex mismatch"
	}
	return ""
}

func containsAll(haystack, needles []string) bool {
	for _, needle := range needles {
		if !slices.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func containsAny(haystack, needles []string) bool {
	for _, needle := range needles {
		if slices.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
