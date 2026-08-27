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
