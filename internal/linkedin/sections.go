package linkedin

import (
	"regexp"
	"strings"

	"github.com/ayushsharma/linkedin-profile-api/internal/model"
)

// This file maps the semantic outline onto typed profile fields.
//
// Sections are identified by a view name or a rendered heading, because that
// is what survives LinkedIn re-partitioning the cards. The cards below the top
// card are currently served as profileCardsBelowActivityPart1..Part7 with no
// stable relationship between a part number and the section inside it, so
// keying on the part number would break on any reshuffle.
//
// Within a section, each list row is an instrumented sub-block whose lines
// follow LinkedIn's fixed visual order: title, subtitle, dates, location, then
// description. Lines are classified by pattern (a date range looks like a date
// range) rather than purely by index, so a missing optional line does not
// shift every subsequent field.

const (
	monthPattern = `(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*`
	pointPattern = `(?:` + monthPattern + `\s+\d{4}|\d{4})`
)

var (
	dateRangeRE = regexp.MustCompile(`(?i)\b(` + pointPattern + `)\s*[-\x{2013}\x{2014}]\s*(` + pointPattern + `|present)\b`)
	singleDate  = regexp.MustCompile(`(?i)^(?:issued\s+)?(` + pointPattern + `)$`)
	durationRE  = regexp.MustCompile(`(?i)\b(\d+\s*(?:yrs?|years?)(?:\s*\d+\s*(?:mos?|months?))?|\d+\s*(?:mos?|months?))\b`)
	proficiency = regexp.MustCompile(`(?i)(native or bilingual|full professional|professional working|limited working|elementary)\s*(proficiency)?`)
)

var employmentTypes = map[string]bool{
	"full-time": true, "part-time": true, "self-employed": true, "freelance": true,
	"contract": true, "internship": true, "apprenticeship": true, "seasonal": true,
	"temporary": true,
}

var workplaceTypes = [...]string{"on-site", "hybrid", "remote"}

// SectionHeadings maps a rendered heading to a canonical section key.
var SectionHeadings = map[string]string{
	"about":                       "about",
	"experience":                  "experience",
	"education":                   "education",
	"licenses & certifications":   "certifications",
	"licenses and certifications": "certifications",
	"certifications":              "certifications",
	"skills":                      "skills",
	"languages":                   "languages",
	"projects":                    "projects",
	"volunteering":                "volunteering",
	"volunteer experience":        "volunteering",
	"publications":                "publications",
	"honors & awards":             "honors",
	"honors and awards":           "honors",
	"courses":                     "courses",
	"organizations":               "organizations",
	"recommendations":             "recommendations",
	"interests":                   "interests",
	"patents":                     "patents",
	"test scores":                 "test_scores",
	"causes":                      "causes",
	"activity":                    "activity",
	"featured":                    "featured",
	"services":                    "services",
	"highlights":                  "highlights",
}

// SectionViewNames maps LinkedIn's own view names to section keys. Checked
// before heading text because it is language-independent.
var SectionViewNames = map[string]string{
	"profile-card-about":           "about",
	"profile-card-experience":      "experience",
	"profile-card-education":       "education",
	"profile-card-certifications":  "certifications",
	"profile-card-skills":          "skills",
	"profile-card-languages":       "languages",
	"profile-card-projects":        "projects",
	"profile-card-volunteering":    "volunteering",
	"profile-card-publications":    "publications",
	"profile-card-honors":          "honors",
	"profile-card-courses":         "courses",
	"profile-card-organizations":   "organizations",
	"profile-card-recommendations": "recommendations",
	"profile-card-interests":       "interests",
	"profile-card-recent-activity": "activity",
	"profile-card-featured":        "featured",
}

// noise is chrome that shows up inside cards and is never content.
var noise = map[string]bool{
	"…see more": true, "see more": true, "show all": true, "show less": true,
	"…more": true, "more": true, "·": true, "•": true, "|": true, "": true,
	"loading": true, "endorse": true, "add": true, "edit": true,
}

var noisePrefixes = [...]string{"show all ", "see all ", "view all ", "loading "}

// IsNoise reports whether a rendered line is UI chrome rather than content.
func IsNoise(line string) bool {
	lowered := strings.ToLower(strings.TrimSpace(line))
	if noise[lowered] {
		return true
	}
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// CleanLines trims and drops chrome.
func CleanLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" || IsNoise(line) {
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	return out
}

// ParseDates reads a rendered date line into a DateRange, or nil.
func ParseDates(line string) *model.DateRange {
	if match := dateRangeRE.FindStringSubmatch(line); match != nil {
		start, end := strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
		result := &model.DateRange{Text: line, Start: start}
		if strings.EqualFold(end, "present") {
			result.IsCurrent = true
		} else {
			result.End = end
		}
		if duration := durationRE.FindStringSubmatch(line); duration != nil {
			result.Duration = strings.TrimSpace(duration[1])
		}
		return result
	}
	if match := singleDate.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
		return &model.DateRange{Text: line, Start: strings.TrimSpace(match[1])}
	}
	return nil
}

// LooksLikeLocation reports whether a line reads as a place.
func LooksLikeLocation(line string) bool {
	lowered := strings.ToLower(line)
	for _, workplace := range workplaceTypes {
		if strings.Contains(lowered, workplace) {
			return true
		}
	}
	// LinkedIn's legacy metro format has no comma: "Greater Seattle Area",
	// "San Francisco Bay Area". Anchored to the end so an "Area Manager" title
	// is not mistaken for a place.
	if strings.HasSuffix(lowered, " area") && len(line) < 60 {
		return true
	}
	// "Bengaluru, Karnataka, India" -- comma separated, short, no verbs.
	return strings.Count(line, ",") >= 1 && len(line) < 90 && ParseDates(line) == nil
}

// SplitEmploymentType turns "Acme Corp · Full-time" into its two parts.
func SplitEmploymentType(line string) (string, string) {
	index := strings.LastIndex(line, "·")
	if index < 0 {
		return line, ""
	}
	head, tail := line[:index], strings.TrimSpace(line[index+len("·"):])
	if employmentTypes[strings.ToLower(tail)] {
		return strings.TrimSpace(strings.TrimRight(head, " ·")), tail
	}
	return line, ""
}

// SectionKey returns the canonical key for a card.
//
// Only a view name or an actual heading counts. Matching on any text line
// would let the global footer's "About" link masquerade as the About card --
// it ships in the same response and would win on document order.
func SectionKey(block *Block) string {
	if key, ok := SectionViewNames[strings.ToLower(strings.TrimSpace(block.Name))]; ok {
		return key
	}
	for _, heading := range block.Headings {
		if key, ok := SectionHeadings[strings.ToLower(strings.TrimSpace(heading))]; ok {
			return key
		}
	}
	return ""
}

// EntityBlocks returns the blocks of a section that look like list rows.
//
// Rows are not reliably direct children. Education nests them one level down
// (section -> wrapper -> lockup per school), so reading only the immediate
// children yields a single block holding every school at once, which maps to
// one entity with all of them glued together. Descending to the level that
// actually holds several rows is what separates them.
//
// A section can also expose both a wrapper and its rows, which would then
// produce the wrapper's merged entity alongside a duplicate of each real row.
// Exact repeats and containing wrappers are both dropped.
func EntityBlocks(section *Block) []*Block {
	candidates := collectRows(section)

	lines := make([][]string, len(candidates))
	for i, block := range candidates {
		lines[i] = CleanLines(block.AllTexts())
	}

	rows := make([]*Block, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for i, block := range candidates {
		key := strings.Join(lines[i], "\x00")
		if seen[key] {
			continue
		}
		wrapper := false
		for j := range candidates {
			if i != j && len(lines[j]) < len(lines[i]) && containsRun(lines[i], lines[j]) {
				wrapper = true
				break
			}
		}
		if wrapper {
			continue
		}
		seen[key] = true
		rows = append(rows, block)
	}
	return rows
}

// collectRows descends past wrappers to the level that holds several rows.
func collectRows(parent *Block) []*Block {
	var rows []*Block
	for _, child := range parent.Children {
		if len(CleanLines(child.AllTexts())) == 0 {
			continue
		}
		if inner := collectRows(child); len(inner) > 1 {
			rows = append(rows, inner...)
			continue
		}
		rows = append(rows, child)
	}
	return rows
}

// containsRun reports whether needle appears in haystack as a contiguous run.
func containsRun(haystack, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		match := true
		for offset, line := range needle {
			if haystack[start+offset] != line {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// ParseEntity maps one list row onto an Entity.
func ParseEntity(block *Block) (model.Entity, bool) {
	lines := CleanLines(block.AllTexts())
	if len(lines) == 0 {
		return model.Entity{}, false
	}

	entity := model.Entity{RawLines: append([]string(nil), lines...)}
	remaining := make([]string, 0, len(lines))
	for _, line := range lines {
		if entity.DateRange == nil {
			if dates := ParseDates(line); dates != nil {
				entity.DateRange = dates
				continue
			}
		}
		remaining = append(remaining, line)
	}

	if len(remaining) > 0 {
		entity.Title = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) > 0 {
		subtitle, employment := SplitEmploymentType(remaining[0])
		// Only consume as a subtitle if it is not obviously a location.
		if !(LooksLikeLocation(subtitle) && len(remaining) == 1) {
			entity.Subtitle, entity.EmploymentType = subtitle, employment
			remaining = remaining[1:]
		}
	}
	for i, line := range remaining {
		if LooksLikeLocation(line) {
			entity.Location = line
			remaining = append(remaining[:i:i], remaining[i+1:]...)
			break
		}
	}
	if len(remaining) > 0 {
		entity.Description = strings.Join(remaining, "\n")
	}

	if images := block.AllImages(); len(images) > 0 {
		logo := ToModelImage(images[0])
		entity.Logo = &logo
	}
	entity.Links = block.Links
	return entity, true
}

// ParseEntities maps every list row in a section.
//
// Two shapes occur. Most sections give one instrumented sub-block per row --
// education renders an education-lockup-view per school -- and those are read
// directly. The collapsed Experience card does not: every role's lines are
// emitted as one flat list on the card itself, and the only per-row children
// are logo blocks carrying an image and no text. Falling back to flat
// segmentation is what keeps Experience from mapping to nothing at all.
func ParseEntities(section *Block) []model.Entity {
	blocks := EntityBlocks(section)
	if len(blocks) == 0 {
		return parseFlatEntities(section)
	}
	var out []model.Entity
	for _, block := range blocks {
		entity, ok := ParseEntity(block)
		if !ok || (entity.Title == "" && entity.Subtitle == "") {
			continue
		}
		out = append(out, entity)
	}
	return out
}

// parseFlatEntities splits a section's own lines into rows.
//
// A date range closes a row. LinkedIn renders each collapsed row as title,
// subtitle, dates -- so the date is the last line of its row and the next line
// begins the next one. Rows are then handed to the same pattern-based mapper
// the sub-block path uses, so a row with no dates still maps its title and
// subtitle.
//
// Location and description only appear on the "show all" detail screen, which
// this service does not follow, so they do not appear in these flat lists. If
// LinkedIn ever inlines them the trailing lines would attach to the following
// row; meta.warnings and raw_lines are the signal that has happened.
func parseFlatEntities(section *Block) []model.Entity {
	lines := withoutHeadings(CleanLines(section.AllTexts()))
	if len(lines) == 0 {
		return nil
	}

	var rows [][]string
	current := make([]string, 0, 4)
	for _, line := range lines {
		current = append(current, line)
		if ParseDates(line) != nil {
			rows = append(rows, current)
			current = make([]string, 0, 4)
		}
	}
	if len(current) > 0 {
		rows = append(rows, current)
	}

	// A date closes a row, but a location is rendered after it -- so a row's
	// trailing location lands at the head of the next one, where it would be
	// read as that row's title. Satya Nadella's Microsoft location became the
	// job title of his next role. Hand such a leading location back.
	for i := 1; i < len(rows); i++ {
		for len(rows[i]) > 1 && LooksLikeLocation(rows[i][0]) {
			rows[i-1] = append(rows[i-1], rows[i][0])
			rows[i] = rows[i][1:]
		}
	}

	// Logos are siblings of the text, one per row and in the same order.
	logos := make([]Image, 0, len(rows))
	for _, child := range section.Children {
		if images := child.AllImages(); len(images) > 0 && len(CleanLines(child.AllTexts())) == 0 {
			logos = append(logos, images[0])
		}
	}

	out := make([]model.Entity, 0, len(rows))
	for i, row := range rows {
		entity, ok := ParseEntity(&Block{Texts: row})
		if !ok || (entity.Title == "" && entity.Subtitle == "") {
			continue
		}
		if i < len(logos) {
			logo := ToModelImage(logos[i])
			entity.Logo = &logo
		}
		out = append(out, entity)
	}
	return out
}

// ParseSkills maps a skills card.
func ParseSkills(section *Block) []model.Skill {
	var out []model.Skill
	seen := map[string]bool{}
	for _, block := range entityBlocksOrSelf(section) {
		lines := withoutHeadings(CleanLines(block.AllTexts()))
		if len(lines) == 0 || seen[lines[0]] {
			continue
		}
		seen[lines[0]] = true
		out = append(out, model.Skill{Name: lines[0], Detail: strings.Join(lines[1:], " · ")})
	}
	return out
}

// ParseLanguages maps a languages card.
func ParseLanguages(section *Block) []model.Language {
	var out []model.Language
	seen := map[string]bool{}
	for _, block := range entityBlocksOrSelf(section) {
		lines := withoutHeadings(CleanLines(block.AllTexts()))
		if len(lines) == 0 || seen[lines[0]] {
			continue
		}
		seen[lines[0]] = true
		language := model.Language{Name: lines[0]}
		for _, line := range lines[1:] {
			if proficiency.MatchString(line) {
				language.Proficiency = line
				break
			}
		}
		out = append(out, language)
	}
	return out
}

// ParseInterests maps an interests card.
func ParseInterests(section *Block) []string {
	var out []string
	seen := map[string]bool{}
	for _, block := range EntityBlocks(section) {
		lines := withoutHeadings(CleanLines(block.AllTexts()))
		if len(lines) == 0 || seen[lines[0]] {
			continue
		}
		seen[lines[0]] = true
		out = append(out, lines[0])
	}
	return out
}

// AsSection wraps a recognised but unmapped card.
func AsSection(key string, block *Block) model.Section {
	section := model.Section{Key: key, Entities: ParseEntities(block), Lines: CleanLines(block.AllTexts())}
	if len(block.Headings) > 0 {
		section.Heading = block.Headings[0]
	}
	return section
}

// ToModelImage converts an outline image to the response shape.
func ToModelImage(image Image) model.Image {
	converted := model.Image{
		URL:      image.URL(400),
		AssetURN: image.AssetURN,
		Label:    image.Label,
	}
	for _, rendition := range image.Renditions {
		converted.Renditions = append(converted.Renditions, model.Rendition(rendition))
	}
	return converted
}

func entityBlocksOrSelf(section *Block) []*Block {
	if blocks := EntityBlocks(section); len(blocks) > 0 {
		return blocks
	}
	return []*Block{section}
}

func withoutHeadings(lines []string) []string {
	out := lines[:0]
	for _, line := range lines {
		if _, isHeading := SectionHeadings[strings.ToLower(line)]; isHeading {
			continue
		}
		out = append(out, line)
	}
	return out
}
