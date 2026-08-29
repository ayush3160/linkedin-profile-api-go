package linkedin

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ayushsharma/linkedin-profile-api/internal/model"
)

var (
	titleSuffix   = regexp.MustCompile(`\s*\|\s*LinkedIn\s*$`)
	degreeRE      = regexp.MustCompile(`(?i)^[·•]?\s*(1st|2nd|3rd|3rd\+)\s*$`)
	countRE       = regexp.MustCompile(`^[\d,.]+\+?[KMB]?$`)
	followersRE   = regexp.MustCompile(`(?i)^([\d,.]+\+?[KMB]?)\s+followers?$`)
	connectionsRE = regexp.MustCompile(`(?i)^([\d,.]+\+?)\s+connections?$`)
	topCardChrome = regexp.MustCompile(`(?i)^(contact info|connections?|followers?|message|connect|follow|following|more|pending|save to pdf|about this profile|mutual connections?|is a mutual connection|view .+ verifications?|open to|add profile section)$`)
)

const (
	photoMarker = "profile-displayphoto"
	coverMarker = "profile-displaybackgroundimage"
)

// entitySections map onto a typed list field of Profile.
var entitySections = map[string]bool{
	"experience": true, "education": true, "certifications": true, "projects": true,
	"volunteering": true, "publications": true, "honors": true, "courses": true,
	"organizations": true, "recommendations": true,
}

var passthroughSections = map[string]bool{
	"activity": true, "featured": true, "services": true, "highlights": true,
	"patents": true, "test_scores": true, "causes": true,
}

// ParseProfile turns {cardName: flightText} into a structured profile.
func ParseProfile(vanity string, cards map[string]string) (model.Profile, model.Meta) {
	meta := model.Meta{}
	profile := model.Profile{
		ProfileURL: fmt.Sprintf("https://www.linkedin.com/in/%s/", vanity),
		VanityName: vanity,
	}

	names := make([]string, 0, len(cards))
	for name := range cards {
		names = append(names, name)
		meta.BytesDownloade += len(cards[name])
	}
	sort.Strings(names)
	meta.CardsReturned = names

	// "shell" first: it carries identity and the top card, and later cards
	// should not overwrite what it established.
	sort.SliceStable(names, func(i, j int) bool { return names[i] == "shell" && names[j] != "shell" })

	outlines := make([]*Block, 0, len(names))
	for _, name := range names {
		doc, err := ParseDocument(cards[name])
		if err != nil {
			meta.CardsFailed = append(meta.CardsFailed, name)
			meta.Warnings = append(meta.Warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		outline := BuildOutline(doc)
		outlines = append(outlines, outline)
		if name == "shell" {
			identity := ProfileIdentity(ModelStates(doc))
			profile.ProfileID = identity.MemberID
			profile.NetworkDistance = identity.NetworkDistance
			applyTopCard(&profile, outline)
		}
	}

	var sectionsFound []string
	seenSection := map[string]bool{}
	for _, outline := range outlines {
		for _, block := range outline.Walk() {
			if block.Component == "__root__" {
				continue
			}
			key := SectionKey(block)
			if key == "" {
				continue
			}
			if seenSection[key] && !entitySections[key] {
				continue
			}
			applySection(&profile, key, block)
			if !seenSection[key] {
				seenSection[key] = true
				sectionsFound = append(sectionsFound, key)
			}
		}
	}

	if profile.Followers == "" {
		applyFollowers(&profile, outlines)
	}
	meta.SectionsFound = sectionsFound
	if profile.Name == "" {
		meta.Warnings = append(meta.Warnings, "could not determine the member's name")
	}
	return profile, meta
}

func applyTopCard(profile *model.Profile, outline *Block) {
	profile.Name = memberName(outline)
	profile.Headline = headline(outline, profile.Name)

	top := outline.First("profile-top-card")
	if top == nil {
		top = outline.First("topCard")
	}
	var lines []string
	if top != nil {
		lines = CleanLines(top.AllTexts())
	}
	profile.Location = location(lines, profile.Name, profile.Headline)
	profile.Connections = connections(lines)
	profile.IsVerified = len(outline.Find("verified-badge", "verification-icon")) > 0
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "open to work") {
			profile.OpenToWork = true
			break
		}
	}

	photo, cover := images(outline)
	profile.ProfilePicture, profile.BackgroundImage = photo, cover
}

func memberName(outline *Block) string {
	// The <title> is the most reliable source: "<Name> | LinkedIn".
	for _, text := range outline.AllTexts() {
		if strings.HasSuffix(text, "| LinkedIn") {
			if name := strings.TrimSpace(titleSuffix.ReplaceAllString(text, "")); name != "" {
				return name
			}
		}
	}
	for _, name := range [...]string{"profile-sticky-header", "verified-badge"} {
		if block := outline.First(name); block != nil {
			if lines := CleanLines(block.AllTexts()); len(lines) > 0 {
				return lines[0]
			}
		}
	}
	return ""
}

func headline(outline *Block, name string) string {
	// The sticky header renders exactly [name, headline] and nothing else.
	if sticky := outline.First("profile-sticky-header"); sticky != nil {
		for _, line := range CleanLines(sticky.AllTexts()) {
			if line != name {
				return line
			}
		}
	}
	top := outline.First("profile-top-card")
	if top == nil {
		return ""
	}
	for _, line := range CleanLines(top.AllTexts()) {
		if line == name || degreeRE.MatchString(line) || topCardChrome.MatchString(line) {
			continue
		}
		if countRE.MatchString(line) || len(line) < 3 {
			continue
		}
		return line
	}
	return ""
}

func location(lines []string, name, head string) string {
	usable := func(line string) bool {
		if line == name || line == head || degreeRE.MatchString(line) || topCardChrome.MatchString(line) {
			return false
		}
		return !countRE.MatchString(line) && len(line) >= 2
	}
	// LinkedIn renders location immediately before the "Contact info" link.
	for i, line := range lines {
		if line != "Contact info" {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if usable(lines[j]) {
				return lines[j]
			}
		}
		break
	}
	for _, line := range lines {
		if !usable(line) {
			continue
		}
		if strings.Count(line, ",") >= 1 || (len(line) < 60 && !strings.Contains(line, " ")) {
			return line
		}
	}
	return ""
}

func connections(lines []string) string {
	for i, line := range lines {
		if match := connectionsRE.FindStringSubmatch(line); match != nil {
			return match[1]
		}
		// Rendered as two adjacent runs: "500+" then "connections".
		if strings.HasPrefix(strings.ToLower(line), "connection") && i > 0 && countRE.MatchString(lines[i-1]) {
			return lines[i-1]
		}
	}
	return ""
}

func applyFollowers(profile *model.Profile, outlines []*Block) {
	for _, outline := range outlines {
		for _, line := range outline.AllTexts() {
			if match := followersRE.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				profile.Followers = match[1]
				return
			}
		}
	}
}

func images(outline *Block) (photo, cover *model.Image) {
	var photoImage, coverImage *Image
	if member := outline.First("profile-top-card-member-photo"); member != nil {
		if found := member.AllImages(); len(found) > 0 {
			photoImage = &found[0]
		}
	}
	top := outline.First("profile-top-card")
	if top == nil {
		top = outline
	}
	for i, image := range top.AllImages() {
		if coverImage == nil && strings.Contains(image.RootURL, coverMarker) {
			coverImage = &top.AllImages()[i]
		}
		if photoImage == nil && strings.Contains(image.RootURL, photoMarker) {
			photoImage = &top.AllImages()[i]
		}
	}
	if photoImage != nil {
		photoImage.Label = "profile photo"
		converted := ToModelImage(*photoImage)
		photo = &converted
	}
	if coverImage != nil {
		coverImage.Label = "cover photo"
		converted := ToModelImage(*coverImage)
		cover = &converted
	}
	return photo, cover
}

func applySection(profile *model.Profile, key string, block *Block) {
	switch {
	case key == "about":
		if profile.About == "" {
			profile.About = aboutText(block)
		}
	case key == "skills":
		for _, skill := range ParseSkills(block) {
			if !strings.EqualFold(skill.Name, "skills") {
				profile.Skills = append(profile.Skills, skill)
			}
		}
	case key == "languages":
		for _, language := range ParseLanguages(block) {
			if !strings.EqualFold(language.Name, "languages") {
				profile.Languages = append(profile.Languages, language)
			}
		}
	case key == "interests":
		profile.Interests = append(profile.Interests, ParseInterests(block)...)
	case entitySections[key]:
		entities := ParseEntities(block)
		if len(entities) == 0 {
			return
		}
		// A section may legitimately arrive split across cards, so repeats are
		// applied rather than skipped. But Walk() also visits nested levels,
		// and an outer block plus the card block resolve to the same rows --
		// which is why every school used to be listed twice. Rows already
		// recorded for this section are dropped.
		entities = withoutSeenEntities(profile, key, entities)
		if len(entities) == 0 {
			return
		}
		switch key {
		case "experience":
			profile.Experience = append(profile.Experience, entities...)
		case "education":
			profile.Education = append(profile.Education, entities...)
		case "certifications":
			profile.Certifications = append(profile.Certifications, entities...)
		case "projects":
			profile.Projects = append(profile.Projects, entities...)
		case "volunteering":
			profile.Volunteering = append(profile.Volunteering, entities...)
		case "publications":
			profile.Publications = append(profile.Publications, entities...)
		case "honors":
			profile.Honors = append(profile.Honors, entities...)
		case "courses":
			profile.Courses = append(profile.Courses, entities...)
		case "organizations":
			profile.Organizations = append(profile.Organizations, entities...)
		case "recommendations":
			profile.Recommendations = append(profile.Recommendations, entities...)
		}
	case passthroughSections[key]:
		section := AsSection(key, block)
		if len(section.Lines) > 0 || len(section.Entities) > 0 {
			profile.OtherSections = append(profile.OtherSections, section)
		}
	}
}

func aboutText(block *Block) string {
	var parts []string
	for _, line := range CleanLines(block.AllTexts()) {
		if _, isHeading := SectionHeadings[strings.ToLower(strings.TrimSpace(line))]; isHeading {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n\n")
}

// entityFingerprint identifies a row by the lines it was built from.
func entityFingerprint(entity model.Entity) string {
	if len(entity.RawLines) > 0 {
		return strings.Join(entity.RawLines, "\x00")
	}
	return entity.Title + "\x00" + entity.Subtitle
}

// withoutSeenEntities drops rows already present in the section.
func withoutSeenEntities(profile *model.Profile, key string, entities []model.Entity) []model.Entity {
	existing := map[string]bool{}
	for _, entity := range entitiesFor(profile, key) {
		existing[entityFingerprint(entity)] = true
	}
	out := entities[:0]
	for _, entity := range entities {
		fingerprint := entityFingerprint(entity)
		if existing[fingerprint] {
			continue
		}
		existing[fingerprint] = true
		out = append(out, entity)
	}
	return out
}

// entitiesFor returns the slice a section key writes into.
func entitiesFor(profile *model.Profile, key string) []model.Entity {
	switch key {
	case "experience":
		return profile.Experience
	case "education":
		return profile.Education
	case "certifications":
		return profile.Certifications
	case "projects":
		return profile.Projects
	case "volunteering":
		return profile.Volunteering
	case "publications":
		return profile.Publications
	case "honors":
		return profile.Honors
	case "courses":
		return profile.Courses
	case "organizations":
		return profile.Organizations
	case "recommendations":
		return profile.Recommendations
	}
	return nil
}
