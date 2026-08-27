package linkedin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayushsharma/linkedin-profile-api/internal/model"
	"github.com/ayushsharma/linkedin-profile-api/internal/testdata"
)

func parsed(t *testing.T) (model.Profile, model.Meta) {
	t.Helper()
	return ParseProfile(testdata.Vanity, testdata.AllCards())
}

func TestTopCardFields(t *testing.T) {
	profile, _ := parsed(t)
	checks := map[string][2]string{
		"name":        {profile.Name, "Ada Lovelace"},
		"headline":    {profile.Headline, "Mathematician | First computer programmer"},
		"location":    {profile.Location, "London, England, United Kingdom"},
		"connections": {profile.Connections, "500+"},
		"vanity":      {profile.VanityName, testdata.Vanity},
		"url":         {profile.ProfileURL, "https://www.linkedin.com/in/" + testdata.Vanity + "/"},
	}
	for field, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", field, pair[0], pair[1])
		}
	}
}

// The top card renders every degree variant; only model state says which is
// live, and it carries the member id too.
func TestIdentityComesFromModelState(t *testing.T) {
	profile, _ := parsed(t)
	if profile.ProfileID != testdata.MemberID {
		t.Errorf("profile_id = %q", profile.ProfileID)
	}
	if profile.NetworkDistance != "2nd" {
		t.Errorf("network_distance = %q, want 2nd", profile.NetworkDistance)
	}
}

func TestImages(t *testing.T) {
	profile, _ := parsed(t)
	if profile.ProfilePicture == nil || !strings.Contains(profile.ProfilePicture.URL, "profile-displayphoto") {
		t.Errorf("profile_picture = %+v", profile.ProfilePicture)
	}
	if profile.BackgroundImage == nil || !strings.Contains(profile.BackgroundImage.URL, "profile-displaybackgroundimage") {
		t.Errorf("background_image = %+v", profile.BackgroundImage)
	}
	if got := len(profile.ProfilePicture.Renditions); got != 2 {
		t.Errorf("renditions = %d, want 2", got)
	}
}

func TestAboutIsJoinedAcrossRuns(t *testing.T) {
	profile, _ := parsed(t)
	for _, want := range []string{"Analytical engine enthusiast.", "Wrote the first algorithm."} {
		if !strings.Contains(profile.About, want) {
			t.Errorf("about missing %q; got %q", want, profile.About)
		}
	}
}

func TestExperienceEntities(t *testing.T) {
	profile, _ := parsed(t)
	if len(profile.Experience) != 2 {
		t.Fatalf("experience count = %d, want 2", len(profile.Experience))
	}
	first := profile.Experience[0]
	if first.Title != "Head of Analytics" {
		t.Errorf("title = %q", first.Title)
	}
	if first.Subtitle != "Analytical Engine Co" {
		t.Errorf("subtitle = %q", first.Subtitle)
	}
	if first.EmploymentType != "Full-time" {
		t.Errorf("employment_type = %q", first.EmploymentType)
	}
	if first.DateRange == nil || first.DateRange.Start != "Jan 2020" || !first.DateRange.IsCurrent {
		t.Errorf("date_range = %+v", first.DateRange)
	}
	if first.DateRange.Duration != "5 yrs 8 mos" {
		t.Errorf("duration = %q", first.DateRange.Duration)
	}
	if first.Location != "London, United Kingdom · Hybrid" {
		t.Errorf("location = %q", first.Location)
	}
	if first.Description != "Built the note G translation pipeline." {
		t.Errorf("description = %q", first.Description)
	}
	if len(first.RawLines) == 0 {
		t.Error("raw_lines should always be populated")
	}
}

func TestSkillsAndLanguages(t *testing.T) {
	profile, _ := parsed(t)
	wantSkills := []string{"Mathematics", "Algorithm Design"}
	if len(profile.Skills) != len(wantSkills) {
		t.Fatalf("skills = %+v", profile.Skills)
	}
	for i, want := range wantSkills {
		if profile.Skills[i].Name != want {
			t.Errorf("skill %d = %q, want %q", i, profile.Skills[i].Name, want)
		}
	}
	if profile.Skills[0].Detail != "Endorsed by 12 colleagues" {
		t.Errorf("skill detail = %q", profile.Skills[0].Detail)
	}
	if len(profile.Languages) != 2 || profile.Languages[0].Name != "English" ||
		profile.Languages[0].Proficiency != "Native or bilingual proficiency" {
		t.Errorf("languages = %+v", profile.Languages)
	}
}

func TestMetaReportsWhatWasParsed(t *testing.T) {
	_, meta := parsed(t)
	found := map[string]bool{}
	for _, key := range meta.SectionsFound {
		found[key] = true
	}
	for _, want := range []string{"about", "experience", "skills", "languages"} {
		if !found[want] {
			t.Errorf("section %q not found (have %v)", want, meta.SectionsFound)
		}
	}
	if meta.BytesDownloade == 0 {
		t.Error("bytes_downloaded should be set")
	}
	if len(meta.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", meta.Warnings)
	}
}

func TestUnparseableCardIsReportedNotFatal(t *testing.T) {
	cards := testdata.AllCards()
	cards["profileCardsBelowActivityPart3"] = "garbage, not flight"
	profile, meta := ParseProfile(testdata.Vanity, cards)
	if profile.Name != "Ada Lovelace" {
		t.Errorf("a bad card should not sink the rest; name = %q", profile.Name)
	}
	if len(meta.CardsFailed) != 1 || meta.CardsFailed[0] != "profileCardsBelowActivityPart3" {
		t.Errorf("cards_failed = %v", meta.CardsFailed)
	}
	if len(meta.Warnings) == 0 {
		t.Error("a failed card should warn")
	}
}

func TestDiscoverCardsFromShell(t *testing.T) {
	cards, err := DiscoverCards(testdata.Shell(), false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, card := range cards {
		names[card.ShortName()] = true
	}
	for _, want := range []string{"profileCardsExperienceOnly", "profileCardsAboveActivity"} {
		if !names[want] {
			t.Errorf("card %q not discovered (have %v)", want, names)
		}
	}
	if names[ActivityCard] {
		t.Error("activity card should be opt-in (it is megabytes)")
	}
	if names["pymkRecommendedEntitySection"] {
		t.Error("recommendation sections are not profile data")
	}
	if got := cards[0].RequestedArgument.Len(); got == 0 {
		t.Error("requestedArguments should be carried through verbatim")
	}
}

func TestActivityCardIsOptIn(t *testing.T) {
	cards, err := DiscoverCards(testdata.Shell(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if card.ShortName() == ActivityCard {
			return
		}
	}
	t.Error("activity card missing when includeActivity is true")
}

// Optional: run against a real capture dumped with tools/hardump.
func TestRealCapture(t *testing.T) {
	dir := os.Getenv("LINKEDIN_FIXTURES")
	if dir == "" {
		t.Skip("set LINKEDIN_FIXTURES to a tools/hardump output directory")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.flight"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no .flight files in %s", dir)
	}
	cards := map[string]string{}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".flight")
		cards[name] = string(body)
	}
	profile, meta := ParseProfile("real-capture", cards)
	if profile.Name == "" {
		t.Fatalf("no name parsed; warnings: %v", meta.Warnings)
	}
	t.Logf("parsed %q (%s) sections=%v", profile.Name, profile.Headline, meta.SectionsFound)
}
