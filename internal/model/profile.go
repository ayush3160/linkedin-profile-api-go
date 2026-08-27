// Package model is the API response schema.
//
// Every profile field is optional. A LinkedIn profile is a sparse document --
// most people have no certifications, many have no About -- and the viewing
// account's network distance changes what is rendered at all. Absent means
// "not present on the page we were served", which is deliberately not the same
// as "the member has none".
package model

// Rendition is one size of an image.
type Rendition struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// Image is a profile or cover photo.
type Image struct {
	URL        string      `json:"url,omitempty"`
	AssetURN   string      `json:"asset_urn,omitempty"`
	Label      string      `json:"label,omitempty"`
	Renditions []Rendition `json:"renditions,omitempty"`
}

// DateRange is a rendered date span, parsed where possible.
type DateRange struct {
	Text      string `json:"text,omitempty"`
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	IsCurrent bool   `json:"is_current"`
	Duration  string `json:"duration,omitempty"`
}

// Entity is one row of a list section: a job, a degree, a certificate.
type Entity struct {
	Title          string     `json:"title,omitempty"`
	Subtitle       string     `json:"subtitle,omitempty"`
	EmploymentType string     `json:"employment_type,omitempty"`
	DateRange      *DateRange `json:"date_range,omitempty"`
	Location       string     `json:"location,omitempty"`
	Description    string     `json:"description,omitempty"`
	Skills         []string   `json:"skills,omitempty"`
	Logo           *Image     `json:"logo,omitempty"`
	Links          []string   `json:"links,omitempty"`
	// RawLines is every text line of this entity in page order, before field
	// mapping, so nothing is lost when a heuristic mismaps.
	RawLines []string `json:"raw_lines,omitempty"`
}

// Skill is a listed skill and any endorsement detail.
type Skill struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// Language is a listed language and proficiency.
type Language struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency,omitempty"`
}

// Section is a recognised card with no typed mapping yet.
type Section struct {
	Key      string   `json:"key"`
	Heading  string   `json:"heading,omitempty"`
	Entities []Entity `json:"entities,omitempty"`
	Lines    []string `json:"lines,omitempty"`
}

// Profile is the structured profile.
type Profile struct {
	ProfileURL string `json:"profile_url"`
	VanityName string `json:"vanity_name"`
	ProfileID  string `json:"profile_id,omitempty"`

	Name     string `json:"name,omitempty"`
	Headline string `json:"headline,omitempty"`
	Location string `json:"location,omitempty"`
	About    string `json:"about,omitempty"`

	Connections     string `json:"connections,omitempty"`
	Followers       string `json:"followers,omitempty"`
	NetworkDistance string `json:"network_distance,omitempty"`
	IsVerified      bool   `json:"is_verified"`
	OpenToWork      bool   `json:"open_to_work"`

	ProfilePicture  *Image `json:"profile_picture,omitempty"`
	BackgroundImage *Image `json:"background_image,omitempty"`

	Experience      []Entity `json:"experience,omitempty"`
	Education       []Entity `json:"education,omitempty"`
	Certifications  []Entity `json:"certifications,omitempty"`
	Projects        []Entity `json:"projects,omitempty"`
	Volunteering    []Entity `json:"volunteering,omitempty"`
	Publications    []Entity `json:"publications,omitempty"`
	Honors          []Entity `json:"honors,omitempty"`
	Courses         []Entity `json:"courses,omitempty"`
	Organizations   []Entity `json:"organizations,omitempty"`
	Recommendations []Entity `json:"recommendations,omitempty"`

	Skills    []Skill    `json:"skills,omitempty"`
	Languages []Language `json:"languages,omitempty"`
	Interests []string   `json:"interests,omitempty"`

	OtherSections []Section `json:"other_sections,omitempty"`
}

// Meta reports what actually happened during a fetch.
type Meta struct {
	CardsReturned  []string `json:"cards_returned,omitempty"`
	CardsFailed    []string `json:"cards_failed,omitempty"`
	SectionsFound  []string `json:"sections_found,omitempty"`
	BytesDownloade int      `json:"bytes_downloaded"`
	DurationMS     int64    `json:"duration_ms"`
	Cached         bool     `json:"cached"`
	Warnings       []string `json:"warnings,omitempty"`
}

// ProfileResponse is the /profile body.
type ProfileResponse struct {
	Profile Profile `json:"profile"`
	Meta    Meta    `json:"meta"`
}

// ErrorResponse is the shape of every error body.
type ErrorResponse struct {
	Error  string `json:"error"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// HealthResponse is the /health body.
type HealthResponse struct {
	Status            string `json:"status"`
	Version           string `json:"version"`
	SessionConfigured bool   `json:"session_configured"`
	CacheEntries      int    `json:"cache_entries"`
	UptimeSeconds     int64  `json:"uptime_seconds"`
}
