package linkedin

import (
	"sort"
	"strings"
)

// This file turns a Flight document into a semantic outline of the rendered
// page.
//
// Why an outline rather than direct field extraction: the RSC payload is a UI
// tree, not a data model. There is no {"firstName": ...} anywhere. What the
// tree does carry reliably is LinkedIn's own instrumentation -- every card and
// list entity is wrapped in a component declaring a
// viewTrackingSpecs.viewName ("profile-top-card", "profile-card-about",
// "profile-card-experience") or an observabilityIdentifier. Those labels are
// stable across redeploys in a way that CSS class hashes and row ids are not,
// so they become the section boundaries.
//
// Three facts about where content actually lives, each established against a
// real capture:
//
//  1. Text is in props.textProps.children for SDUI Text components and in
//     props.children for raw HTML tags. It is not in one uniform slot, and
//     content props are arbitrarily named (renderedToolbar, rendererWorkspace,
//     initialContent, newComponent), so the walker descends everything and
//     excludes behaviour by rule instead of allow-listing content.
//  2. SDUI wraps attributed runs in react fragments and <span>/<br>, so a text
//     subtree must stay in text mode once entered. Resetting at the wrapper
//     silently drops whole paragraphs -- an entire About body, in the capture
//     that caught this.
//  3. Content is also parked inside ReplaceComponent action payloads. Walking
//     from row 0 alone finds 2 strings in the activity response; also scanning
//     rows the render tree never reached finds 136.

const maxDepth = 900

// behaviourProps carry behaviour, state, tracking or styling -- never content.
var behaviourProps = map[string]bool{
	"triggers": true, "visibilityTriggers": true, "action": true, "actions": true,
	"onAppear": true, "onReappear": true, "onDisappear": true, "onBackOverride": true,
	"onClientRequestFailureAction": true, "modelStates": true, "trackingScope": true,
	"persistence": true, "requestedArguments": true, "clientBreadcrumbPointers": true,
	"key": true, "className": true, "style": true, "componentKey": true,
	"componentkey": true, "dataFetchMeta": true, "jiraLabels": true, "customEntries": true,
	"screenHash": true, "screenId": true, "treeId": true, "errorMessage": true,
	"sduiProductUsecase": true, "legacyPemRatio": true, "data-sdui-screen": true,
	"impressionThresholds": true,
}

// textTags are raw HTML tags whose children are human-readable text.
var textTags = map[string]bool{
	"p": true, "span": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"strong": true, "em": true, "b": true, "i": true, "li": true, "a": true,
	"button": true, "time": true, "title": true, "figcaption": true, "label": true,
	"dd": true, "dt": true,
}

var ariaProps = [...]string{"aria-label", "a11yText", "alt"}

type textKind int

const (
	kindNone textKind = iota
	kindText
	kindHeading
)

// Rendition is one size of a LinkedIn vector image.
type Rendition struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// Image is a LinkedIn vector image: rootUrl + rendition suffix is a real URL.
type Image struct {
	RootURL    string
	Renditions []Rendition
	AssetURN   string
	Label      string
}

// URL returns the smallest rendition at or above minWidth, else the largest.
func (i Image) URL(minWidth int) string {
	if len(i.Renditions) == 0 {
		return i.RootURL
	}
	sorted := append([]Rendition(nil), i.Renditions...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Width < sorted[b].Width })
	for _, r := range sorted {
		if r.Width >= minWidth {
			return r.URL
		}
	}
	return sorted[len(sorted)-1].URL
}

// Token identifies an image for deduplication.
func (i Image) Token() string {
	if i.AssetURN != "" {
		return i.AssetURN
	}
	return i.RootURL
}

// Block is one instrumented region of the page and everything rendered inside.
type Block struct {
	Name      string // viewTrackingSpecs.viewName
	Component string // observabilityIdentifier / data-sdui-component
	Texts     []string
	Aria      []string
	Headings  []string
	Links     []string
	Images    []Image
	Children  []*Block
}

// Label is the block's most specific identifier.
func (b *Block) Label() string {
	switch {
	case b.Name != "":
		return b.Name
	case b.Component != "":
		return b.Component
	default:
		return "?"
	}
}

// Walk yields this block and every descendant, depth first in document order.
func (b *Block) Walk() []*Block {
	out := make([]*Block, 0, 32)
	var visit func(*Block)
	visit = func(blk *Block) {
		out = append(out, blk)
		for _, child := range blk.Children {
			visit(child)
		}
	}
	visit(b)
	return out
}

// Find returns every block whose name or component contains any of names.
func (b *Block) Find(names ...string) []*Block {
	var out []*Block
	for _, blk := range b.Walk() {
		name, component := strings.ToLower(blk.Name), strings.ToLower(blk.Component)
		for _, want := range names {
			want = strings.ToLower(want)
			if strings.Contains(name, want) || strings.Contains(component, want) {
				out = append(out, blk)
				break
			}
		}
	}
	return out
}

// First returns the first match of Find, or nil.
func (b *Block) First(names ...string) *Block {
	if found := b.Find(names...); len(found) > 0 {
		return found[0]
	}
	return nil
}

// AllTexts returns the texts of this block and every descendant, in order.
func (b *Block) AllTexts() []string {
	var out []string
	for _, blk := range b.Walk() {
		out = append(out, blk.Texts...)
	}
	return out
}

// AllImages returns the images of this block and every descendant.
func (b *Block) AllImages() []Image {
	var out []Image
	for _, blk := range b.Walk() {
		out = append(out, blk.Images...)
	}
	return out
}

// Outline parses a Flight stream and builds its semantic outline.
func Outline(text string) (*Block, error) {
	doc, err := ParseDocument(text)
	if err != nil {
		return nil, err
	}
	return BuildOutline(doc), nil
}

// BuildOutline walks a document and produces a tree of Blocks.
func BuildOutline(doc *Document) *Block {
	b := &builder{doc: doc, visited: make(map[string]bool, 64)}
	root := &Block{Component: "__root__"}
	seen := make(map[string]bool, 32)
	b.descend(doc.Root(), root, seen, 0, kindNone)

	// Only rows the render tree never reached. Scanning every row instead
	// would re-walk the same components through a dozen entry points and
	// duplicate whole cards.
	ids := make([]string, 0, len(doc.Rows))
	for id, row := range doc.Rows {
		if row.Kind == RowModel && id != "0" && !b.visited[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		seen := map[string]bool{id: true}
		b.descend(doc.Rows[id].Value, root, seen, 0, kindNone)
	}

	dedupe(root)
	collapse(root, make(map[string]bool, 64))
	return root
}

type builder struct {
	doc     *Document
	visited map[string]bool
}

func (b *builder) descend(node any, blk *Block, seen map[string]bool, depth int, kind textKind) {
	if depth > maxDepth || node == nil {
		return
	}
	switch value := node.(type) {
	case string:
		b.descendString(value, blk, seen, depth, kind)
	case []any:
		b.descendList(value, blk, seen, depth, kind)
	case *Object:
		b.descendObject(value, blk, seen, depth, kind)
	}
}

func (b *builder) descendString(node string, blk *Block, seen map[string]bool, depth int, kind textKind) {
	if strings.HasPrefix(node, "$") {
		resolved, id, ok := b.doc.Deref(node, seen)
		if !ok {
			return
		}
		b.visited[id] = true
		seen[id] = true
		b.descend(resolved, blk, seen, depth+1, kind)
		delete(seen, id) // per-path cycle detection, not global
		return
	}
	text := strings.TrimSpace(node)
	if text == "" || kind == kindNone {
		return
	}
	if kind == kindHeading {
		blk.Headings = append(blk.Headings, text)
	}
	blk.Texts = append(blk.Texts, text)
}

func (b *builder) descendList(node []any, blk *Block, seen map[string]bool, depth int, kind textKind) {
	if !IsElement(node) {
		for _, item := range node {
			b.descend(item, blk, seen, depth+1, kind)
		}
		return
	}

	tag := b.resolveTag(node[1], seen)
	props, _ := node[3].(*Object)
	target := openBlock(props, blk)

	for _, prop := range ariaProps {
		if value := props.Str(prop); value != "" && !strings.HasPrefix(value, "$") {
			target.Aria = append(target.Aria, strings.TrimSpace(value))
		}
	}

	if textProps, ok := props.Get("textProps"); ok {
		if tp, ok := textProps.(*Object); ok {
			if children, ok := tp.Get("children"); ok {
				sub := kindText
				if name := tp.Str("tagName"); strings.HasPrefix(name, "h") {
					sub = kindHeading
				}
				b.descend(children, target, seen, depth+1, sub)
			}
		}
	}

	props.Each(func(prop string, value any) {
		if behaviourProps[prop] || prop == "textProps" || prop == "viewTrackingSpecs" ||
			strings.HasSuffix(prop, "Expression") || isAriaProp(prop) {
			return
		}
		// Stay in text mode once inside a text subtree: fragments, spans and
		// <br> wrappers would otherwise break a paragraph into nothing.
		childKind := kindNone
		if kind == kindText || kind == kindHeading {
			childKind = kind
		} else if prop == "children" && textTags[tag] {
			childKind = kindText
		}
		b.descend(value, target, seen, depth+1, childKind)
	})
}

func (b *builder) descendObject(node *Object, blk *Block, seen map[string]bool, depth int, kind textKind) {
	if image, ok := vectorImage(node); ok {
		blk.Images = append(blk.Images, image)
		return
	}
	nodeType := node.Str("$type")
	if nodeType == "proto.sdui.actions.core.NavigateToUrl" {
		if url := node.Str("url"); url != "" {
			blk.Links = append(blk.Links, url)
		}
		return
	}
	if nodeType != "" || node.Has("$case") {
		// Behaviour payload. Content is sometimes parked in here, so keep
		// descending into the slots that hold components.
		for _, slot := range [...]string{"newComponent", "initialContent", "content"} {
			if value, ok := node.Get(slot); ok {
				b.descend(value, blk, seen, depth+1, kind)
			}
		}
		return
	}
	node.Each(func(prop string, value any) {
		if behaviourProps[prop] {
			return
		}
		b.descend(value, blk, seen, depth+1, kind)
	})
}

func (b *builder) resolveTag(nodeType any, seen map[string]bool) string {
	name, ok := nodeType.(string)
	if !ok {
		return "?"
	}
	if !strings.HasPrefix(name, "$") {
		return name
	}
	resolved, id, ok := b.doc.Deref(name, seen)
	if !ok {
		return name
	}
	b.visited[id] = true
	if ref, ok := resolved.(clientRef); ok {
		return ref.Export
	}
	return name
}

// openBlock starts a new Block when an element declares an instrumentation
// label, otherwise content keeps accumulating on the parent.
func openBlock(props *Object, parent *Block) *Block {
	var name string
	if specs, ok := props.Get("viewTrackingSpecs"); ok {
		if obj, ok := specs.(*Object); ok {
			name = obj.Str("viewName")
		}
	}
	component := props.Str("observabilityIdentifier")
	if component == "" {
		component = props.Str("data-sdui-component")
	}
	if name == "" && component == "" {
		return parent
	}
	child := &Block{Name: name, Component: component}
	parent.Children = append(parent.Children, child)
	return child
}

func vectorImage(node *Object) (Image, bool) {
	root := node.Str("rootUrl")
	if root == "" || !strings.Contains(root, "licdn.com") {
		return Image{}, false
	}
	image := Image{RootURL: root, AssetURN: node.Str("assetUrn")}
	if renditions, ok := node.Get("imageRenditions"); ok {
		if list, ok := renditions.([]any); ok {
			for _, item := range list {
				obj, ok := item.(*Object)
				if !ok {
					continue
				}
				suffix := obj.Str("suffixUrl")
				if suffix == "" {
					continue
				}
				image.Renditions = append(image.Renditions, Rendition{
					Width:  intOf(obj, "width"),
					Height: intOf(obj, "height"),
					URL:    root + suffix,
				})
			}
		}
	}
	return image, true
}

func isAriaProp(prop string) bool {
	for _, a := range ariaProps {
		if a == prop {
			return true
		}
	}
	return false
}

// dedupe collapses repeats introduced by the greedy pass, preserving order.
func dedupe(root *Block) {
	for _, blk := range root.Walk() {
		blk.Texts = uniqueStrings(blk.Texts)
		blk.Aria = uniqueStrings(blk.Aria)
		blk.Headings = uniqueStrings(blk.Headings)
		blk.Links = uniqueStrings(blk.Links)
		seen := make(map[string]bool, len(blk.Images))
		images := blk.Images[:0]
		for _, image := range blk.Images {
			if seen[image.Token()] {
				continue
			}
			seen[image.Token()] = true
			images = append(images, image)
		}
		blk.Images = images
	}
}

// collapse drops blocks that render exactly what an earlier block rendered.
// Two genuinely distinct list entities never collide because their text
// differs; top-down order keeps the shallowest copy.
func collapse(block *Block, seen map[string]bool) {
	kept := block.Children[:0]
	for _, child := range block.Children {
		sig := signature(child)
		if seen[sig] {
			continue
		}
		seen[sig] = true
		kept = append(kept, child)
	}
	block.Children = kept
	for _, child := range kept {
		collapse(child, seen)
	}
}

func signature(block *Block) string {
	var sb strings.Builder
	sb.WriteString(block.Name)
	sb.WriteByte(0)
	sb.WriteString(block.Component)
	sb.WriteByte(0)
	for _, text := range block.AllTexts() {
		sb.WriteString(text)
		sb.WriteByte(1)
	}
	sb.WriteByte(0)
	for _, image := range block.AllImages() {
		sb.WriteString(image.Token())
		sb.WriteByte(1)
	}
	sb.WriteByte(0)
	for _, link := range block.Links {
		sb.WriteString(link)
		sb.WriteByte(1)
	}
	return sb.String()
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]bool, len(values))
	out := values[:0]
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
