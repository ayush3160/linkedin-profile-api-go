// Package testdata builds synthetic Flight fixtures.
//
// Real captures are not committed: they contain a third party's personal data
// and run to megabytes. These builders reproduce the wire shapes verified
// against a real capture -- row grammar, $L references, textProps text
// components, viewTrackingSpecs view names, vector images, model states and
// AsyncComponentRequest card advertisements -- at a size you can read.
//
// To exercise the parser against a real capture instead, dump one with
// tools/hardump and point LINKEDIN_FIXTURES at the directory.
package testdata

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Vanity and MemberID identify the synthetic member.
const (
	Vanity     = "ada-lovelace-1815"
	MemberID   = "ACoAAATESTTESTTESTTESTTESTTESTTESTTESTT"
	cardPrefix = "com.linkedin.sdui.generated.profile.dsl.impl."
)

// Row renders one Flight row.
func Row(id string, payload any) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return id + ":" + string(encoded)
}

// ClientRow renders a client-component reference row.
func ClientRow(id, export string) string {
	return fmt.Sprintf(`%s:I["%s",[],"%s"]`, id, strings.Repeat("0", 32), export)
}

// Element renders a React element: ["$", tag, key, props].
func Element(tag string, props map[string]any) []any {
	return []any{"$", tag, nil, props}
}

// KeyedElement is Element with a React key, as fragments in attributed runs use.
func KeyedElement(tag, key string, props map[string]any) []any {
	return []any{"$", tag, key, props}
}

// Text renders an SDUI Text component; content lives under textProps.children.
func Text(value any) []any {
	return Element("$L9", map[string]any{
		"textProps": map[string]any{
			"fontFamily": "sans", "fontSize": "small",
			"children": normalise(value),
		},
	})
}

// Heading renders an SDUI Text component with an <h2> tagName.
func Heading(value string) []any {
	return Element("$L9", map[string]any{
		"textProps": map[string]any{
			"fontFamily": "sans", "fontSize": "xlarge", "tagName": "h2",
			"children": []any{value},
		},
	})
}

// Tracked wraps children in a component declaring viewTrackingSpecs.viewName.
func Tracked(viewName string, children any) []any {
	return Element("div", map[string]any{
		"viewTrackingSpecs": map[string]any{"viewName": viewName, "contentTrackingId": "x=="},
		"children":          children,
	})
}

// VectorImage renders LinkedIn's image model: rootUrl plus suffixed renditions.
func VectorImage(slug string, widths ...int) map[string]any {
	if len(widths) == 0 {
		widths = []int{100, 400}
	}
	renditions := make([]any, 0, len(widths))
	for _, width := range widths {
		renditions = append(renditions, map[string]any{
			"width": width, "height": width,
			"suffixUrl": fmt.Sprintf("scale_%d_%d/0/1?e=1&v=beta&t=t", width, width),
		})
	}
	return map[string]any{
		"rootUrl":         "https://media.licdn.com/dms/image/v2/ABC123/" + slug + "-",
		"imageRenditions": renditions,
		"assetUrn":        "urn:li:digitalmediaAsset:" + slug,
	}
}

// State renders one SDUI model-state entry, double-nested as on the wire.
func State(id, value string) map[string]any {
	return map[string]any{
		"key":   map[string]any{"key": map[string]any{"value": map[string]any{"$case": "id", "id": id}}},
		"value": map[string]any{"$case": "stringValue", "stringValue": value},
	}
}

// AsyncCard renders one advertised lazily-loaded card.
func AsyncCard(shortName string) map[string]any {
	return map[string]any{
		"$type":          "proto.sdui.actions.core.AsyncComponentRequest",
		"newComponentId": cardPrefix + shortName,
		"requestedArguments": map[string]any{
			"$type":              "proto.sdui.actions.requests.RequestedArguments",
			"requestedStateKeys": []any{},
			"payload": map[string]any{
				"isSelfView": false,
				"vanityName": Vanity,
				"replaceableSectionArgs": map[string]any{
					"vanityName": Vanity, "vieweeProfileId": MemberID, "isSelfView": false,
				},
			},
		},
	}
}

// Shell is the navigation response: shell, top card, sticky header and the
// card advertisements the client discovers.
func Shell() string {
	topCard := Tracked("profile-top-card", []any{
		Tracked("profile-top-card-member-photo",
			Element("$La", map[string]any{
				"a11yText": "Profile photo", "image": VectorImage("profile-displayphoto"),
			})),
		Element("$La", map[string]any{
			"a11yText": "Cover photo", "image": VectorImage("profile-displaybackgroundimage"),
		}),
		Tracked("profile-top-card-verified-badge", Text("Ada Lovelace")),
		// Every degree variant ships, because the card also carries UI for
		// states the viewer is not in. Only model state says which is live.
		Text("· 1st"),
		Text("· 2nd"),
		Text("Mathematician | First computer programmer"),
		Text("London, England, United Kingdom"),
		Text("Contact info"),
		Text("500+"),
		Text("connections"),
	})
	sticky := Tracked("profile-sticky-header", []any{
		Text("Ada Lovelace"),
		Text("Mathematician | First computer programmer"),
	})

	cards := []any{}
	for _, name := range []string{
		"profileCardsAboveActivity", "profileCardsExperienceOnly",
		"profileCardsBelowActivityPart1WithoutExp", "profileCardsBelowActivityPart2",
		"profileCardsActivity", "pymkRecommendedEntitySection",
	} {
		cards = append(cards, AsyncCard(name))
	}
	states := []any{
		State("profile_network_distance_"+MemberID, "Distance2"),
		State("urn:li:fsd_followingState:urn:li:member:1815", "Follow"),
	}

	return strings.Join([]string{
		ClientRow("9", "Text"),
		ClientRow("a", "DesignSystemImage"),
		Row("0", []any{
			map[string]any{"component": "$undefined"},
			map[string]any{"component": []any{"$L1"}, "pageKey": "profile_view_base"},
		}),
		Row("1", Element("div", map[string]any{
			"data-sdui-screen": "com.linkedin.sdui.flagshipnav.profile.Profile",
			"children": []any{
				Element("title", map[string]any{"children": "Ada Lovelace | LinkedIn"}),
				Element("$L2", map[string]any{"modelStates": states, "children": []any{"$L3"}}),
			},
		})),
		Row("2", Element("div", map[string]any{"children": "$L3"})),
		Row("3", []any{sticky, topCard, map[string]any{"actions": cards}}),
	}, "\n")
}

// About is the card carrying the About section, with the body split into
// attributed runs wrapped in react fragments -- the shape that used to drop
// the entire paragraph.
func About() string {
	body := Tracked("profile-card-about", []any{
		Heading("About"),
		Text([]any{[]any{
			KeyedElement("$c", "0", map[string]any{
				"children": []any{nil, "Analytical engine enthusiast."},
			}),
			KeyedElement("$c", "1", map[string]any{
				"children": []any{Element("br", map[string]any{}), "Wrote the first algorithm."},
			}),
		}}),
	})
	return strings.Join([]string{
		ClientRow("9", "Text"),
		`c:"$Sreact.fragment"`,
		Row("0", Element("div", map[string]any{
			"data-sdui-component": cardPrefix + "profileCardsAboveActivity",
			"children":            []any{body},
		})),
	}, "\n")
}

// Experience is the experience card with two roles.
func Experience() string {
	job := func(title, company, dates, location, description string) []any {
		return Tracked("profile-component-entity", []any{
			Text(title), Text(company), Text(dates), Text(location), Text(description),
		})
	}
	card := Element("div", map[string]any{
		"viewTrackingSpecs": map[string]any{"viewName": "profile-card-experience"},
		"children": []any{
			Heading("Experience"),
			job("Head of Analytics", "Analytical Engine Co · Full-time",
				"Jan 2020 - Present · 5 yrs 8 mos", "London, United Kingdom · Hybrid",
				"Built the note G translation pipeline."),
			job("Research Assistant", "Royal Society",
				"Jun 2018 - Dec 2019 · 1 yr 7 mos", "Remote",
				"Studied Bernoulli numbers."),
		},
	})
	return strings.Join([]string{ClientRow("9", "Text"), Row("0", card)}, "\n")
}

// Skills is the skills card.
func Skills() string {
	card := Element("div", map[string]any{
		"viewTrackingSpecs": map[string]any{"viewName": "profile-card-skills"},
		"children": []any{
			Heading("Skills"),
			Tracked("profile-component-entity", []any{Text("Mathematics"), Text("Endorsed by 12 colleagues")}),
			Tracked("profile-component-entity", []any{Text("Algorithm Design")}),
		},
	})
	return strings.Join([]string{ClientRow("9", "Text"), Row("0", card)}, "\n")
}

// Languages is the languages card.
func Languages() string {
	card := Element("div", map[string]any{
		"viewTrackingSpecs": map[string]any{"viewName": "profile-card-languages"},
		"children": []any{
			Heading("Languages"),
			Tracked("profile-component-entity", []any{Text("English"), Text("Native or bilingual proficiency")}),
			Tracked("profile-component-entity", []any{Text("French"), Text("Professional working proficiency")}),
		},
	})
	return strings.Join([]string{ClientRow("9", "Text"), Row("0", card)}, "\n")
}

// AllCards is every fixture keyed the way the client returns them.
func AllCards() map[string]string {
	return map[string]string{
		"shell":                                    Shell(),
		"profileCardsAboveActivity":                About(),
		"profileCardsExperienceOnly":               Experience(),
		"profileCardsBelowActivityPart1WithoutExp": Skills(),
		"profileCardsBelowActivityPart2":           Languages(),
	}
}

func normalise(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{value}
}
