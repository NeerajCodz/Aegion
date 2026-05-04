package retention

import (
	"errors"
	"time"
)

// TierType defines the storage tier for data.
type TierType string

const (
	TierHot  TierType = "hot"
	TierWarm TierType = "warm"
	TierCold TierType = "cold"
)

// CompressionType defines the compression algorithm for warm storage.
type CompressionType string

const (
	CompressionSnappy CompressionType = "snappy"
	CompressionGzip   CompressionType = "gzip"
	CompressionZstd   CompressionType = "zstd"
	CompressionNone   CompressionType = "none"
)

// TierConfig defines the configuration for a storage tier.
type TierConfig struct {
	TTLDays     int             `yaml:"ttl_days"`
	Enabled     bool            `yaml:"enabled"`
	Storage     string          `yaml:"storage"` // local, s3, s3_iceberg
	Compression CompressionType `yaml:"compression,omitempty"`
}

// RetentionPolicy defines how long data is kept in each tier.
type RetentionPolicy struct {
	// Default policies for all categories
	DefaultPolicy string     `yaml:"default_policy"`
	Hot           TierConfig `yaml:"hot"`
	Warm          TierConfig `yaml:"warm"`
	Cold          TierConfig `yaml:"cold"`

	// Category-specific overrides
	Categories map[string]CategoryRetention `yaml:"categories,omitempty"`
}

// CategoryRetention defines retention settings for a specific event category.
type CategoryRetention struct {
	HotDays  int `yaml:"hot_days"`
	WarmDays int `yaml:"warm_days"`
	ColdDays int `yaml:"cold_days"`
}

// Validate checks if the policy is valid.
func (p *RetentionPolicy) Validate() error {
	if p.DefaultPolicy == "" {
		p.DefaultPolicy = "tiered"
	}

	// Validate tier configs
	if p.Hot.TTLDays == 0 {
		p.Hot.TTLDays = 7
	}
	if p.Warm.TTLDays == 0 {
		p.Warm.TTLDays = 90
	}
	if p.Cold.TTLDays == 0 {
		p.Cold.TTLDays = 730 // ~2 years
	}

	// Ensure TTLs are in increasing order
	if p.Hot.TTLDays >= p.Warm.TTLDays {
		return errors.New("hot tier TTL must be less than warm tier TTL")
	}
	if p.Warm.TTLDays >= p.Cold.TTLDays {
		return errors.New("warm tier TTL must be less than cold tier TTL")
	}

	// Validate compression type
	if p.Warm.Compression == "" {
		p.Warm.Compression = CompressionSnappy
	}

	return nil
}

// DefaultRetentionPolicy returns a sensible default policy.
func DefaultRetentionPolicy() *RetentionPolicy {
	return &RetentionPolicy{
		DefaultPolicy: "tiered",
		Hot: TierConfig{
			TTLDays:     7,
			Enabled:     true,
			Storage:     "local",
			Compression: CompressionNone,
		},
		Warm: TierConfig{
			TTLDays:     90,
			Enabled:     true,
			Storage:     "s3",
			Compression: CompressionSnappy,
		},
		Cold: TierConfig{
			TTLDays:     730,
			Enabled:     true,
			Storage:     "s3_iceberg",
			Compression: CompressionNone,
		},
		Categories: make(map[string]CategoryRetention),
	}
}

// GetTierForTimestamp determines which tier a record should be in based on its timestamp.
func (p *RetentionPolicy) GetTierForTimestamp(category string, recordTime time.Time) TierType {
	now := time.Now()
	ageInDays := int(now.Sub(recordTime).Hours() / 24)

	// Get category-specific TTLs or use defaults
	categoryRetention, exists := p.Categories[category]
	hotDays := p.Hot.TTLDays
	warmDays := p.Warm.TTLDays

	if exists {
		if categoryRetention.HotDays > 0 {
			hotDays = categoryRetention.HotDays
		}
		if categoryRetention.WarmDays > 0 {
			warmDays = categoryRetention.WarmDays
		}
	}

	if ageInDays < hotDays {
		return TierHot
	} else if ageInDays < warmDays {
		return TierWarm
	}
	return TierCold
}

// IsExpired checks if a record has exceeded the maximum retention period.
func (p *RetentionPolicy) IsExpired(category string, recordTime time.Time) bool {
	now := time.Now()
	ageInDays := int(now.Sub(recordTime).Hours() / 24)

	categoryRetention, exists := p.Categories[category]
	coldDays := p.Cold.TTLDays

	if exists && categoryRetention.ColdDays > 0 {
		coldDays = categoryRetention.ColdDays
	}

	return ageInDays >= coldDays
}

// GetTierConfig returns the configuration for a specific tier.
func (p *RetentionPolicy) GetTierConfig(tier TierType) *TierConfig {
	switch tier {
	case TierHot:
		return &p.Hot
	case TierWarm:
		return &p.Warm
	case TierCold:
		return &p.Cold
	default:
		return nil
	}
}

// NextTier returns the next tier a record should transition to.
func (p *RetentionPolicy) NextTier(current TierType) TierType {
	switch current {
	case TierHot:
		return TierWarm
	case TierWarm:
		return TierCold
	default:
		return TierCold
	}
}
