package urlparse

import "testing"

func TestExtractVanityAcceptsEveryURLShape(t *testing.T) {
	tests := map[string]string{
		"https://www.linkedin.com/in/ada-lovelace-1815/":    "ada-lovelace-1815",
		"https://www.linkedin.com/in/ada-lovelace-1815":     "ada-lovelace-1815",
		"https://in.linkedin.com/in/ada?trk=public_profile": "ada",
		"http://linkedin.com/in/ada/":                       "ada",
		"www.linkedin.com/in/ada/":                          "ada",
		"/in/ada":                                           "ada",
		"in/ada/":                                           "ada",
		"ada-lovelace-1815":                                 "ada-lovelace-1815",
		"https://www.linkedin.com/in/%C3%A9lise-dupont/":    "élise-dupont",
		"  https://www.linkedin.com/in/ada/  ":              "ada",
	}
	for input, want := range tests {
		got, err := ExtractVanity(input)
		if err != nil {
			t.Errorf("ExtractVanity(%q) errored: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ExtractVanity(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExtractVanityRejectsNonProfileURLs(t *testing.T) {
	for _, input := range []string{
		"", "   ",
		"https://example.com/in/ada",
		"https://linkedin.evil.com/in/ada",
		"https://www.linkedin.com/company/acme",
		"https://www.linkedin.com/in/",
		"https://www.linkedin.com/feed/",
	} {
		if got, err := ExtractVanity(input); err == nil {
			t.Errorf("ExtractVanity(%q) = %q, want an error", input, got)
		}
	}
}

func TestProfileURLRoundTrip(t *testing.T) {
	got, err := ExtractVanity(ProfileURL("ada"))
	if err != nil || got != "ada" {
		t.Errorf("round trip = %q, %v", got, err)
	}
}
