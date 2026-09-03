package linkedin

import (
	"regexp"
	"strings"

	"github.com/ayush3160/linkedin-profile-api-go/internal/model"
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
	// SDUI renders layout and icon hints as text nodes. On the Experience card
	// these opened rows titled "icon" with a subtitle of "inlineBlock".
	"img": true, "icon": true, "logobrand": true, "inlineblock": true,
	"blockquote": true, "center": true, "horizontal": true, "vertical": true,
	"button": true, "true": true, "false": true,
	// A badge LinkedIn renders inside a role, not part of the role.
	"helped me get this job": true,
}

var noisePrefixes = [...]string{"show all ", "see all ", "view all ", "loading ", "linkedin helped me get"}

// SDUI emits its own i18n plumbing as rendered text: the binding key for a
// count, the ICU template that formats it, and the locale it resolved in. On
// the Skills card these arrive before any real skill, so the first of them was
// being read as the skill's name.
var (
	icuTemplate = regexp.MustCompile(`^\{[0-9]+,\s*(plural|number|select)`)
	localeCode  = regexp.MustCompile(`^[a-z]{2}_[A-Z]{2}$`)
	bindingKey  = regexp.MustCompile(`^\S+-(count|index|state)$`)
	// Component ids for lazily expanded text, e.g.
	// expandable_text_block_auto-component-f63c8fcb-...
	componentID = regexp.MustCompile(`auto-component-[0-9a-f-]{8,}$`)
	// Spacing and layout tokens: 7x, flexStart, WIDTH_AND_HEIGHT.
	styleToken = regexp.MustCompile(`^(?:[0-9]+x|flexStart|flexEnd|iconDisabled|backgroundOffset|isolate|[A-Z]+(?:_[A-Z]+)+)$`)
)

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
	trimmed := strings.TrimSpace(line)
	// An ICU template carries spaces; the identifier shapes below never do,
	// and requiring that keeps real prose out of the noise set.
	if icuTemplate.MatchString(trimmed) || localeCode.MatchString(trimmed) {
		return true
	}
	if strings.Contains(trimmed, " ") {
		return false
	}
	return bindingKey.MatchString(trimmed) ||
		componentID.MatchString(trimmed) ||
		styleToken.MatchString(trimmed)
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

// credentialLine matches the metadata LinkedIn renders under a certificate.
var credentialLine = regexp.MustCompile(`(?i)^credential (id|url)\b|^show credential`)

// continuesPreviousRow reports whether a line belongs to the row before it
// rather than opening a new one.
//
// Everything a row renders after its date behaves this way. Prose is
// recognised by shape -- a bullet, a sentence ending, or a length no title
// has -- which keeps a short standalone entry like "Lakeside School" opening
// its own row.
func continuesPreviousRow(line string) bool {
	return LooksLikeLocation(line) || credentialLine.MatchString(line) || looksLikeProse(line)
}

// looksLikeProse reports whether a line reads as a sentence rather than a
// label. A title or an employer never does.
func looksLikeProse(line string) bool {
	switch {
	case strings.HasPrefix(line, "•"), strings.HasPrefix(line, "- "):
		return true
	case strings.HasSuffix(line, "."), len([]rune(line)) > 80:
		return true
	}
	return false
}

// bareDuration matches a span with no dates in it -- "18 yrs 1 mo". LinkedIn
// renders one under a company name when a member held several roles there,
// as a header above the individual roles.
var bareDuration = regexp.MustCompile(`(?i)^\d+\s+(yrs?|years?|mos?|months?)(\s+\d+\s+(mos?|months?))?$`)

// namesItsOwnEmployer reports whether a row carries an employer of its own,
// rather than inheriting one from a group header.
func namesItsOwnEmployer(lines []string) bool {
	labels := 0
	for _, line := range lines {
		// A date, a description, an employment type and the role's own
		// location all describe the role. Only a second *label* means the row
		// named an employer -- counting the location ended the group a row
		// early, and the next role lost its employer again.
		if ParseDates(line) != nil || looksLikeProse(line) ||
			employmentTypes[strings.ToLower(line)] || LooksLikeLocation(line) {
			continue
		}
		labels++
	}
	return labels >= 2
}

// closesRow reports whether a line ends the list row it belongs to.
//
// Most rows end with a bare date. Honors and certifications end with an issuer
// line instead -- "Issued by Economic Times · Jan 2016" -- which singleDate
// will not match because it is anchored. Without this, every award on a card
// merged into one entity.
func closesRow(line string) bool {
	if ParseDates(line) != nil {
		return true
	}
	if index := strings.LastIndex(line, "·"); index >= 0 {
		return ParseDates(strings.TrimSpace(line[index+len("·"):])) != nil
	}
	return false
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
	name := strings.ToLower(strings.TrimSpace(block.Name))
	if key, ok := SectionViewNames[name]; ok {
		return key
	}
	// Interests are not rendered inside the interests card. Each followed
	// company, school, newsletter or influencer is its own interest-tab-*
	// block alongside it, so the card itself holds nothing but its heading.
	if strings.HasPrefix(name, "interest-tab-") {
		return "interests"
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

// rowChrome names blocks that sit inside a section but are not list rows: the
// button for an attached document, a company logo link. Their text is a file
// name, so treating them as rows made Experience report
// "Confirmation of Internship Offer.pdf" as a job and Licenses report the
// attachment names instead of the certificates -- while the real content, held
// in the card's own text, was skipped because these children looked like rows.
var rowChrome = [...]string{"media-button", "logo-click", "see-media", "media-roll-up"}

func isRowChrome(name string) bool {
	lowered := strings.ToLower(name)
	for _, chrome := range rowChrome {
		if strings.Contains(lowered, chrome) {
			return true
		}
	}
	return false
}

// contentTexts is AllTexts minus the chrome subtrees. A section's own flat
// list has to exclude the attachment buttons hanging off it, or their file
// names join the list and become entities of their own.
func contentTexts(block *Block) []string {
	out := append([]string(nil), block.Texts...)
	for _, child := range block.Children {
		if isRowChrome(child.Name) {
			continue
		}
		out = append(out, contentTexts(child)...)
	}
	return out
}

// collectRows descends past wrappers to the level that holds several rows.
func collectRows(parent *Block) []*Block {
	var rows []*Block
	for _, child := range parent.Children {
		if isRowChrome(child.Name) {
			continue
		}
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
	var credentials []string
	for _, line := range lines {
		if entity.DateRange == nil {
			if dates := ParseDates(line); dates != nil {
				entity.DateRange = dates
				continue
			}
		}
		// A credential id is metadata, not the issuer. Left in place it takes
		// the subtitle slot on any certificate that renders no issuer line.
		if credentialLine.MatchString(line) {
			credentials = append(credentials, line)
			continue
		}
		remaining = append(remaining, line)
	}

	if len(remaining) > 0 {
		entity.Title = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) > 0 {
		subtitle, employment := SplitEmploymentType(remaining[0])
		// Only consume as a subtitle if it is not obviously a location, and
		// not a sentence -- a grouped role whose employer lives in the card
		// header is followed straight by its description, which would
		// otherwise be read as the company name.
		if !looksLikeProse(subtitle) && !(LooksLikeLocation(subtitle) && len(remaining) == 1) {
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
	if len(credentials) > 0 {
		if entity.Description != "" {
			entity.Description += "\n"
		}
		entity.Description += strings.Join(credentials, "\n")
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
	lines := withoutHeadings(CleanLines(contentTexts(section)))
	if len(lines) == 0 {
		return nil
	}

	// Several roles at one employer render as a header -- company name, total
	// span, location -- followed by the roles themselves, which then carry no
	// company of their own. Read as flat rows the header became an entity
	// titled with the company and subtitled "18 yrs 1 mo", and every role under
	// it lost its employer.
	type row struct {
		lines    []string
		company  string
		location string
	}
	var (
		rows      []row
		current   = make([]string, 0, 4)
		group     string
		groupLoc  string
		afterHead bool
	)
	for _, line := range lines {
		if bareDuration.MatchString(line) && len(current) == 1 {
			group, groupLoc, afterHead = current[0], "", true
			current = current[:0]
			continue
		}
		// The header's own location sits between the span and the first role,
		// where it would otherwise open a row and be read as a job title.
		if afterHead {
			if LooksLikeLocation(line) {
				groupLoc = line
				continue
			}
			afterHead = false
		}
		current = append(current, line)
		if closesRow(line) {
			// The group covers only the roles that follow its header. A row
			// that names its own employer has left it, and leaves before it is
			// recorded -- otherwise it keeps the header's location. Without
			// this the header's company was also stamped onto every later
			// role, which is worse than not grouping at all.
			if group != "" && namesItsOwnEmployer(current) {
				group, groupLoc = "", ""
			}
			rows = append(rows, row{lines: current, company: group, location: groupLoc})
			current = make([]string, 0, 4)
		}
	}
	if len(current) > 0 {
		rows = append(rows, row{lines: current, company: group, location: groupLoc})
	}

	// A date closes a row, but location, description and credential lines are
	// rendered after it -- so a row's tail lands at the head of the next one,
	// where the first of them would be read as that row's title. Satya
	// Nadella's Microsoft location became the job title of his next role, and a
	// job description became a job of its own. Hand those lines back.
	for i := 1; i < len(rows); i++ {
		for len(rows[i].lines) > 0 && continuesPreviousRow(rows[i].lines[0]) {
			rows[i-1].lines = append(rows[i-1].lines, rows[i].lines[0])
			rows[i].lines = rows[i].lines[1:]
		}
	}
	kept := rows[:0]
	for _, r := range rows {
		if len(r.lines) > 0 {
			kept = append(kept, r)
		}
	}
	rows = kept

	// Logos are siblings of the text, one per row and in the same order.
	logos := make([]Image, 0, len(rows))
	for _, child := range section.Children {
		if images := child.AllImages(); len(images) > 0 && len(CleanLines(child.AllTexts())) == 0 {
			logos = append(logos, images[0])
		}
	}

	out := make([]model.Entity, 0, len(rows))
	for i, r := range rows {
		entity, ok := ParseEntity(&Block{Texts: r.lines})
		if !ok || (entity.Title == "" && entity.Subtitle == "") {
			continue
		}
		// A grouped role carries its own title and, at most, an employment
		// type. The employer lives in the header, so it belongs in the
		// subtitle -- and an employment type sitting there is not a company.
		if r.company != "" {
			if entity.Subtitle == "" {
				entity.Subtitle = r.company
			} else if employmentTypes[strings.ToLower(entity.Subtitle)] {
				entity.EmploymentType = entity.Subtitle
				entity.Subtitle = r.company
			}
		}
		if entity.Location == "" && r.location != "" {
			entity.Location = r.location
		}
		if i < len(logos) {
			logo := ToModelImage(logos[i])
			entity.Logo = &logo
		}
		out = append(out, entity)
	}
	return out
}

// skillDetail recognises the lines that describe a skill rather than name one:
// "3 endorsements", or the role it was endorsed through.
var skillDetail = regexp.MustCompile(`(?i)^\d+\+?\s*endorsement|^endorsed by|\sat\s`)

// ParseSkills maps a skills card.
//
// Like Experience, the card renders as one flat list: a skill name followed by
// zero or more lines describing it. Treating each block's first line as a name
// produced a single skill called "Skills<vanity>-count" -- the i18n binding key
// -- carrying every real skill in its detail.
func ParseSkills(section *Block) []model.Skill {
	var out []model.Skill
	seen := map[string]bool{}
	for _, block := range entityBlocksOrSelf(section) {
		for _, line := range withoutHeadings(CleanLines(block.AllTexts())) {
			if skillDetail.MatchString(line) {
				if len(out) > 0 {
					last := &out[len(out)-1]
					if last.Detail == "" {
						last.Detail = line
					} else {
						last.Detail += " · " + line
					}
				}
				continue
			}
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, model.Skill{Name: line})
		}
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

// interestType strips the trailing kind LinkedIn appends to an interest's
// accessible name: "Coinbase, Company" -> "Coinbase".
var interestType = regexp.MustCompile(`,\s*(Company|School|Newsletter|Group|Influencer)$`)

// ParseInterests maps one interest tab, or an interests card that holds its
// entries inline.
func ParseInterests(section *Block) []string {
	var out []string
	seen := map[string]bool{}
	for _, block := range entityBlocksOrSelf(section) {
		lines := withoutHeadings(CleanLines(block.AllTexts()))
		if len(lines) == 0 {
			continue
		}
		name := strings.TrimSpace(interestType.ReplaceAllString(lines[0], ""))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
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
