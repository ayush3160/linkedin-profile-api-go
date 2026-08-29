package linkedin

import "testing"

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
