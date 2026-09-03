package linkedin

import (
	"strings"
	"testing"
)

func TestParseDates(t *testing.T) {
	tests := []struct {
		line     string
		start    string
		end      string
		current  bool
		duration string
	}{
		{"Jan 2023 - Present · 2 yrs 8 mos", "Jan 2023", "", true, "2 yrs 8 mos"},
		{"May 2021 - Aug 2021 · 4 mos", "May 2021", "Aug 2021", false, "4 mos"},
		{"2019 - 2023", "2019", "2023", false, ""},
		{"Sept 2020 - Present", "Sept 2020", "", true, ""},
		{"Issued Mar 2024", "Mar 2024", "", false, ""},
	}
	for _, tc := range tests {
		got := ParseDates(tc.line)
		if got == nil {
			t.Errorf("ParseDates(%q) = nil", tc.line)
			continue
		}
		if got.Start != tc.start || got.End != tc.end ||
			got.IsCurrent != tc.current || got.Duration != tc.duration {
			t.Errorf("ParseDates(%q) = %+v", tc.line, *got)
		}
	}
}

func TestParseDatesRejectsNonDates(t *testing.T) {
	for _, line := range []string{"Head of Analytics", "Acme Corp", ""} {
		if got := ParseDates(line); got != nil {
			t.Errorf("ParseDates(%q) = %+v, want nil", line, *got)
		}
	}
}

func TestSplitEmploymentType(t *testing.T) {
	tests := []struct{ line, company, employment string }{
		{"Acme Corp · Full-time", "Acme Corp", "Full-time"},
		{"Acme · Corp", "Acme · Corp", ""},
		{"Acme Corp", "Acme Corp", ""},
	}
	for _, tc := range tests {
		company, employment := SplitEmploymentType(tc.line)
		if company != tc.company || employment != tc.employment {
			t.Errorf("SplitEmploymentType(%q) = %q, %q", tc.line, company, employment)
		}
	}
}

func TestLooksLikeLocation(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"London, England, United Kingdom", true},
		{"Remote", true},
		{"Bengaluru, Karnataka, India · Hybrid", true},
		{"Mathematician | First computer programmer", false},
	}
	for _, tc := range tests {
		if got := LooksLikeLocation(tc.line); got != tc.want {
			t.Errorf("LooksLikeLocation(%q) = %v", tc.line, got)
		}
	}
}

func TestIsNoise(t *testing.T) {
	for _, line := range []string{"…see more", "Show all 12 experiences", "·", ""} {
		if !IsNoise(line) {
			t.Errorf("IsNoise(%q) = false", line)
		}
	}
	if IsNoise("Head of Analytics") {
		t.Error("real content flagged as noise")
	}
}

// The collapsed Experience card emits every role as one flat text list with no
// text-bearing sub-blocks -- only logo blocks. Reading just the sub-blocks
// found nothing, so Experience mapped to an empty array while the data sat in
// plain sight.
func TestFlatExperienceCardSplitsIntoRows(t *testing.T) {
	section := &Block{
		Name: "profile-card-experience",
		Texts: []string{
			"Experience",
			"Chairman and CEO", "Microsoft", "Feb 2014 - Present · 12 yrs 7 mos", "Greater Seattle Area",
			"Member Board Of Trustees", "University of Chicago", "2018 – Present",
			"Board Member", "Starbucks", "2017 – 2024",
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 3 {
		t.Fatalf("entities = %d, want 3: %+v", len(entities), entities)
	}
	if entities[0].Title != "Chairman and CEO" || entities[0].Subtitle != "Microsoft" {
		t.Errorf("row 0 = %q / %q", entities[0].Title, entities[0].Subtitle)
	}
	// A location is rendered after the date that closes its row, so it arrives
	// at the head of the next one and must be handed back.
	if entities[0].Location != "Greater Seattle Area" {
		t.Errorf("row 0 location = %q, want Greater Seattle Area", entities[0].Location)
	}
	if entities[1].Title != "Member Board Of Trustees" {
		t.Errorf("row 1 title = %q -- the previous row's location leaked in", entities[1].Title)
	}
}

// Education nests its rows under a wrapper. Reading only direct children glued
// every school into one entity; counting the wrapper alongside its rows listed
// each school twice.
func TestNestedRowsAreUnwrappedNotDuplicated(t *testing.T) {
	row1 := &Block{Name: "education-lockup-view", Texts: []string{"Harvard University", "1973 – 1975"}}
	row2 := &Block{Name: "education-lockup-view", Texts: []string{"Lakeside School"}}
	wrapper := &Block{Name: "wrapper", Children: []*Block{row1, row2}}
	section := &Block{Name: "profile-card-education", Children: []*Block{wrapper}}

	entities := ParseEntities(section)
	if len(entities) != 2 {
		t.Fatalf("entities = %d, want 2: %+v", len(entities), entities)
	}
	if entities[0].Title != "Harvard University" || entities[1].Title != "Lakeside School" {
		t.Errorf("titles = %q, %q", entities[0].Title, entities[1].Title)
	}
}

func TestLooksLikeLocationAcceptsMetroAreasButNotTitles(t *testing.T) {
	for _, place := range []string{"Greater Seattle Area", "San Francisco Bay Area", "Bengaluru, Karnataka, India"} {
		if !LooksLikeLocation(place) {
			t.Errorf("LooksLikeLocation(%q) = false, want true", place)
		}
	}
	for _, title := range []string{"Area Manager", "Chairman and CEO", "Founder"} {
		if LooksLikeLocation(title) {
			t.Errorf("LooksLikeLocation(%q) = true, want false", title)
		}
	}
}

// SDUI renders its own i18n plumbing as text. On the Skills card the binding
// key, the ICU template and the locale arrive before any real skill, and the
// first of them was read as the skill's name -- a lone skill called
// "Skills<vanity>-count" carrying every real skill in its detail.
func TestSkillsCardIgnoresI18nPlumbing(t *testing.T) {
	section := &Block{
		Name:     "profile-card-skills",
		Headings: []string{"Skills"},
		Texts: []string{
			"Skillsada-lovelace-count",
			"{0,plural,0#|one# ({0,number,integer})|other# ({0,number,integer})}",
			"en_US",
			"Skills",
			"Next.js", "Full Stack Developer Intern at Unlock Velocity",
			"Web Development", "3 endorsements",
		},
	}
	skills := ParseSkills(section)
	if len(skills) != 2 {
		t.Fatalf("skills = %d, want 2: %+v", len(skills), skills)
	}
	if skills[0].Name != "Next.js" || skills[1].Name != "Web Development" {
		t.Errorf("names = %q, %q", skills[0].Name, skills[1].Name)
	}
	if skills[1].Detail != "3 endorsements" {
		t.Errorf("detail = %q, want 3 endorsements", skills[1].Detail)
	}
}

func TestIsNoiseRejectsBindingKeysAndTemplates(t *testing.T) {
	for _, line := range []string{"Skillsada-count", "en_US", "{0,plural,0#|one# (x)}", "show all 12 skills"} {
		if !IsNoise(line) {
			t.Errorf("IsNoise(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"Next.js", "Web Development", "Chairman and CEO", "Harvard University"} {
		if IsNoise(line) {
			t.Errorf("IsNoise(%q) = true, want false", line)
		}
	}
}

// Interests are not rendered inside the interests card -- that holds only its
// heading. Each followed entity is its own interest-tab-* block beside it, so
// the card must not claim the section and stop there.
func TestInterestTabsAreRecognisedAndNamed(t *testing.T) {
	tab := &Block{Name: "interest-tab-companies", Texts: []string{"Coinbase, Company", "true", "Coinbase", "1,464,706 followers"}}
	if key := SectionKey(tab); key != "interests" {
		t.Fatalf("SectionKey = %q, want interests", key)
	}
	got := ParseInterests(tab)
	if len(got) != 1 || got[0] != "Coinbase" {
		t.Fatalf("interests = %v, want [Coinbase]", got)
	}
	influencer := &Block{Name: "interest-tab-influencers", Texts: []string{"Kunal Shah", "· 2nd", "1,415,218 followers"}}
	if got := ParseInterests(influencer); len(got) != 1 || got[0] != "Kunal Shah" {
		t.Fatalf("interests = %v, want [Kunal Shah]", got)
	}
}

// Honors and certifications end a row with an issuer line rather than a bare
// date -- "Issued by Economic Times · Jan 2016". singleDate is anchored, so it
// never matched and every award on the card merged into one entity.
func TestRowsClosedByAnIssuerLineAreSplit(t *testing.T) {
	section := &Block{
		Name:     "profile-card-honors",
		Headings: []string{"Honors & awards"},
		Texts: []string{
			"Comeback award - Economic Times", "Issued by Economic Times · Jan 2016",
			"Economic Times 40 under 40", "Issued by Times of India · Jan 2016",
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 2 {
		t.Fatalf("entities = %d, want 2: %+v", len(entities), entities)
	}
	if entities[0].Title != "Comeback award - Economic Times" {
		t.Errorf("row 0 title = %q", entities[0].Title)
	}
	if entities[1].Title != "Economic Times 40 under 40" {
		t.Errorf("row 1 title = %q -- both awards merged", entities[1].Title)
	}
}

// "99+ endorsements" was read as a skill name because \d+\s+ cannot cross the
// plus sign.
func TestEndorsementCountIsNeverASkillName(t *testing.T) {
	section := &Block{
		Name:     "profile-card-skills",
		Headings: []string{"Skills"},
		Texts: []string{
			"Marketing Strategy", "Endorsed by Praveen and 3 others who are highly skilled at this",
			"99+ endorsements",
			"Entrepreneurship", "13 endorsements",
		},
	}
	skills := ParseSkills(section)
	names := make([]string, len(skills))
	for i, skill := range skills {
		names[i] = skill.Name
	}
	if len(skills) != 2 || names[0] != "Marketing Strategy" || names[1] != "Entrepreneurship" {
		t.Fatalf("skill names = %v, want [Marketing Strategy Entrepreneurship]", names)
	}
}

// The Licenses & certifications card holds its content in the card's own text,
// with attachment buttons hanging off it whose text is a PDF file name. Those
// buttons looked like list rows, so the card reported the attachments and
// skipped the certificates entirely.
func TestCertificationsComeFromTheCardNotItsAttachments(t *testing.T) {
	section := &Block{
		Name:     "profile-card-licenses-and-certifications",
		Headings: []string{"Licenses & certifications"},
		Texts: []string{
			"Licenses & certifications",
			"TCS-ION Career Edge - Young Professional", "TCS iON", "Issued Jan 2024",
			"TCS ION (Business Etiquette)", "Issued Dec 2023", "Credential ID 66794256907341016",
		},
		Children: []*Block{
			{Name: "license-certifications-lockup-view"},
			{Name: "license-certifications-media-button", Texts: []string{"TCS-ION(Career Edge).pdf"}},
			{Name: "license-certifications-media-button", Texts: []string{"BUSINESS ETIQUETTE.pdf"}},
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 2 {
		t.Fatalf("entities = %d, want 2: %+v", len(entities), entities)
	}
	if entities[0].Title != "TCS-ION Career Edge - Young Professional" || entities[0].Subtitle != "TCS iON" {
		t.Errorf("row 0 = %q / %q", entities[0].Title, entities[0].Subtitle)
	}
	if entities[0].DateRange == nil || entities[0].DateRange.Text != "Issued Jan 2024" {
		t.Errorf("row 0 date = %+v", entities[0].DateRange)
	}
	// A credential id is metadata, not an issuer.
	if entities[1].Subtitle != "" {
		t.Errorf("row 1 subtitle = %q, want empty", entities[1].Subtitle)
	}
	if !strings.Contains(entities[1].Description, "Credential ID") {
		t.Errorf("row 1 description = %q, want the credential id", entities[1].Description)
	}
}

// A job description follows the date that closed its row, so it arrived at the
// head of the next row and opened a job of its own.
func TestJobDescriptionStaysWithItsRole(t *testing.T) {
	section := &Block{
		Name:     "profile-card-experience",
		Headings: []string{"Experience"},
		Texts: []string{
			"Experience",
			"Business Analyst", "Basiq360 · Full-time", "Mar 2024 - Present · 2 yrs",
			"Faridabad, Haryana, India · On-site",
			"Managed day-to-day client interactions, addressing technical issues and new development",
			"• Translated client requirements into technical instructions.",
		},
		Children: []*Block{
			{Name: "experience-see-media-button", Texts: []string{"Offer Letter.pdf", "OFFER LETTER"}},
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 1 {
		t.Fatalf("entities = %d, want 1: %+v", len(entities), entities)
	}
	if entities[0].Title != "Business Analyst" {
		t.Errorf("title = %q", entities[0].Title)
	}
	if !strings.Contains(entities[0].Description, "Translated client requirements") {
		t.Errorf("description = %q", entities[0].Description)
	}
}

func TestStyleTokensAreNotContent(t *testing.T) {
	for _, token := range []string{"icon", "inlineBlock", "img", "logoBrand", "true", "button"} {
		if !IsNoise(token) {
			t.Errorf("IsNoise(%q) = false -- style tokens opened rows titled %q", token, token)
		}
	}
}

// Several roles at one employer render as a header -- company, total span,
// location -- followed by roles that carry no company of their own. Read flat,
// the header became an entity titled with the company and subtitled with the
// span, and every role under it lost its employer.
func TestRolesInheritTheirEmployerFromTheGroupHeader(t *testing.T) {
	section := &Block{
		Name:     "profile-card-experience",
		Headings: []string{"Experience"},
		Texts: []string{
			"Experience",
			"Keploy", "2 yrs 4 mos", "Bangalore, Karnataka, India",
			// Each role carries its own location; that must not be mistaken
			// for naming an employer, or the group ends a row early.
			"SWE", "Full-time", "Jun 2025 - Present · 1 yr 4 mos", "Bangalore, Karnataka, India · On-site",
			"SDE Intern", "Internship", "Sep 2024 - Jun 2025 · 10 mos", "Remote",
			// A role naming its own employer has left the group.
			"Full Stack Developer Intern", "Unlock Velocity", "Sep 2023 - Jun 2024 · 10 mos",
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 3 {
		t.Fatalf("entities = %d, want 3: %+v", len(entities), entities)
	}
	for i, want := range []struct{ title, company string }{
		{"SWE", "Keploy"},
		{"SDE Intern", "Keploy"},
		{"Full Stack Developer Intern", "Unlock Velocity"},
	} {
		if entities[i].Title != want.title || entities[i].Subtitle != want.company {
			t.Errorf("row %d = %q / %q, want %q / %q", i, entities[i].Title, entities[i].Subtitle, want.title, want.company)
		}
	}
	// The employment type is not a company, and the header's location belongs
	// to the roles under it.
	if entities[0].EmploymentType != "Full-time" {
		t.Errorf("employment type = %q", entities[0].EmploymentType)
	}
	// A role's own location wins over the header's.
	if entities[0].Location != "Bangalore, Karnataka, India · On-site" {
		t.Errorf("location = %q", entities[0].Location)
	}
	if entities[1].Location != "Remote" {
		t.Errorf("row 1 location = %q", entities[1].Location)
	}
	// The group must not follow a role that named its own employer.
	if entities[2].Location != "" {
		t.Errorf("row 2 location = %q -- the group's header leaked past its roles", entities[2].Location)
	}
}

// Media roll-ups and the layout tokens inside them are chrome. They were read
// as list rows, producing jobs titled "7x" with a subtitle of "flexStart".
func TestLayoutChromeNeverBecomesAnEntity(t *testing.T) {
	section := &Block{
		Name:     "profile-card-experience",
		Headings: []string{"Experience"},
		Texts:    []string{"Experience", "Founding Partner", "Next Play Ventures", "Jul 2020 - Present · 6 yrs"},
		Children: []*Block{
			{Name: "experience-media-roll-up", Texts: []string{"7x", "flexStart", "iconDisabled", "isolate", "+5"}},
			{Name: "experience-see-media-button", Texts: []string{"7x", "flexStart", "WIDTH_AND_HEIGHT"}},
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 1 {
		t.Fatalf("entities = %d, want 1: %+v", len(entities), entities)
	}
	if entities[0].Title != "Founding Partner" || entities[0].Subtitle != "Next Play Ventures" {
		t.Errorf("row = %q / %q", entities[0].Title, entities[0].Subtitle)
	}
	for _, token := range []string{"7x", "flexStart", "iconDisabled", "isolate", "WIDTH_AND_HEIGHT"} {
		if !IsNoise(token) {
			t.Errorf("IsNoise(%q) = false", token)
		}
	}
	// An ICU template carries spaces and must still be recognised.
	if !IsNoise("{0,plural,0#|one# ({0,number,integer})}") {
		t.Error("ICU template should stay noise")
	}
}

// A grouped role is followed straight by its description, which would be read
// as the company name it does not have.
func TestADescriptionIsNeverTheEmployer(t *testing.T) {
	section := &Block{
		Name:     "profile-card-experience",
		Headings: []string{"Experience"},
		Texts: []string{
			"Experience",
			"Next Play Ventures", "18 yrs 1 mo", "San Francisco Bay Area",
			"Founding Partner", "Jul 2020 - Present · 6 yrs 3 mos",
			"Our mission is to coach and invest in entrepreneurial leaders building purpose-driven organizations.",
		},
	}
	entities := ParseEntities(section)
	if len(entities) != 1 {
		t.Fatalf("entities = %d, want 1: %+v", len(entities), entities)
	}
	if entities[0].Subtitle != "Next Play Ventures" {
		t.Errorf("subtitle = %q, want the employer, not the description", entities[0].Subtitle)
	}
	if !strings.Contains(entities[0].Description, "Our mission") {
		t.Errorf("description = %q", entities[0].Description)
	}
}
