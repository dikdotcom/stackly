package fingerprint

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// DetectionRule represents a single detection pattern
type DetectionRule struct {
	Pattern    string `json:"pattern"`
	Match      string `json:"match,omitempty"`      // contains (default), regex, exact, exists
	Confidence int    `json:"confidence,omitempty"` // default 100
	Name       string `json:"name,omitempty"`       // for meta: tag name
	Header     string `json:"header,omitempty"`     // for headers: header name
}

// Detectors holds all detection methods
type Detectors struct {
	HTML      []DetectionRule `json:"html,omitempty"`
	Headers   []DetectionRule `json:"headers,omitempty"`
	Scripts   []DetectionRule `json:"scripts,omitempty"`
	Cookies   []DetectionRule `json:"cookies,omitempty"`
	URL       []DetectionRule `json:"url,omitempty"`
	JSGlobals []DetectionRule `json:"js_globals,omitempty"`
	CSS       []DetectionRule `json:"css,omitempty"`
	Meta      []DetectionRule `json:"meta,omitempty"`
}

// Technology represents a single technology fingerprint
type Technology struct {
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Website   string    `json:"website,omitempty"`
	Category  string    `json:"category"`
	Icon      string    `json:"icon,omitempty"`
	Detectors Detectors `json:"detectors"`
	Implies   []string  `json:"implies,omitempty"`
	Excludes  []string  `json:"excludes,omitempty"`
}

// Database is the complete fingerprint database
type Database struct {
	Version      string       `json:"version"`
	Updated      string       `json:"updated"`
	Technologies []Technology `json:"technologies"`
}

var (
	db   *Database
	once sync.Once
	mu   sync.RWMutex
)

// Load reads and parses the fingerprint JSON file
func Load(path string) (*Database, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fingerprint file: %w", err)
	}
	var database Database
	if err := json.Unmarshal(data, &database); err != nil {
		return nil, fmt.Errorf("parse fingerprint JSON: %w", err)
	}
	mu.Lock()
	db = &database
	mu.Unlock()
	return &database, nil
}

// GetDatabase returns the loaded database (nil if not loaded)
func GetDatabase() *Database {
	mu.RLock()
	defer mu.RUnlock()
	return db
}

// GetTechnologies returns all technologies
func GetTechnologies() []Technology {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return nil
	}
	return db.Technologies
}

// GetByCategory returns technologies in a specific category
func GetByCategory(category string) []Technology {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return nil
	}
	var result []Technology
	for _, tech := range db.Technologies {
		if tech.Category == category {
			result = append(result, tech)
		}
	}
	return result
}

// FindBySlug returns a technology by its slug
func FindBySlug(slug string) *Technology {
	mu.RLock()
	defer mu.RUnlock()
	if db == nil {
		return nil
	}
	for i := range db.Technologies {
		if db.Technologies[i].Slug == slug {
			return &db.Technologies[i]
		}
	}
	return nil
}
