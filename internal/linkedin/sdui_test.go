package linkedin

import (
	"strings"
	"testing"

	"github.com/ayush3160/linkedin-profile-api-go/internal/testdata"
)

func outlineOf(t *testing.T, flight string) *Block {
	t.Helper()
	root, err := Outline(flight)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	return root
}

func TestViewNamesBecomeBlocks(t *testing.T) {
	root := outlineOf(t, testdata.Shell())
	names := map[string]bool{}
	for _, block := range root.Walk() {
		if block.Name != "" {
			names[block.Name] = true
		}
	}
	for _, want := range []string{"profile-top-card", "profile-sticky-header", "profile-top-card-member-photo"} {
		if !names[want] {
			t.Errorf("missing block %q (have %v)", want, names)
		}
	}
}

// Regression: attributed runs are wrapped in $Sreact.fragment and <br>.
// Resetting text mode at the wrapper drops the entire About body.
func TestTextSurvivesFragmentsAndBreaks(t *testing.T) {
	root := outlineOf(t, testdata.About())
	about := root.First("profile-card-about")
	if about == nil {
		t.Fatal("no profile-card-about block")
	}
	joined := strings.Join(about.AllTexts(), " ")
	for _, want := range []string{"Analytical engine enthusiast.", "Wrote the first algorithm."} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
}

func TestHeadingsAreTrackedSeparately(t *testing.T) {
	root := outlineOf(t, testdata.Experience())
	card := root.First("profile-card-experience")
	if card == nil {
		t.Fatal("no experience card")
	}
	if len(card.Headings) != 1 || card.Headings[0] != "Experience" {
		t.Errorf("headings = %v", card.Headings)
	}
}

func TestVectorImagesBecomeURLs(t *testing.T) {
	root := outlineOf(t, testdata.Shell())
	images := root.AllImages()
	if len(images) == 0 {
		t.Fatal("no images extracted")
	}
	foundPhoto := false
	for _, image := range images {
		url := image.URL(400)
		if !strings.HasPrefix(url, "https://media.licdn.com/") {
			t.Errorf("bad url %q", url)
		}
		if strings.Contains(url, "profile-displayphoto") {
			foundPhoto = true
		}
	}
	if !foundPhoto {
		t.Error("profile photo not found")
	}
}

func TestImageURLPicksSmallestAboveMinWidth(t *testing.T) {
	image := Image{
		RootURL: "https://media.licdn.com/x/",
		Renditions: []Rendition{
			{Width: 800, URL: "big"}, {Width: 100, URL: "small"}, {Width: 400, URL: "mid"},
		},
	}
	if got := image.URL(400); got != "mid" {
		t.Errorf("URL(400) = %q, want mid", got)
	}
	if got := image.URL(2000); got != "big" {
		t.Errorf("URL(2000) = %q, want big (largest available)", got)
	}
}

// The greedy pass reaches components through several rows; without the
// reachability filter the top card appears a dozen times.
func TestBlocksAreNotDuplicatedByGreedyPass(t *testing.T) {
	root := outlineOf(t, testdata.Shell())
	count := 0
	for _, block := range root.Walk() {
		if block.Name == "profile-top-card" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("profile-top-card appears %d times, want 1", count)
	}
}
