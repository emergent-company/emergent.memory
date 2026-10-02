package main

// devgallery_ui.go holds presentation-only helpers for the component gallery
// page. It deliberately contains no templ components and no render call sites:
// the component graph is generated from render topology (cmd/componentgraph),
// so keeping the gallery's component structure here would churn the generated
// graph. Everything in this file is plain data shaping for the template.

// galleryCategoryGroup is one layer's entries grouped under a single category,
// in the catalog's own first-seen order.
type galleryCategoryGroup struct {
	Category string
	Entries  []GalleryEntry
}

// entriesByCategory groups a layer's catalog entries by category. Both the
// category order and the entries within each group follow the catalog order, so
// the rendered page is stable across calls.
func entriesByCategory(entries []GalleryEntry, layer GalleryLayer) []galleryCategoryGroup {
	var order []string
	byCategory := map[string][]GalleryEntry{}
	for _, e := range entries {
		if e.Layer != layer {
			continue
		}
		if _, seen := byCategory[e.Category]; !seen {
			order = append(order, e.Category)
		}
		byCategory[e.Category] = append(byCategory[e.Category], e)
	}
	groups := make([]galleryCategoryGroup, 0, len(order))
	for _, category := range order {
		groups = append(groups, galleryCategoryGroup{Category: category, Entries: byCategory[category]})
	}
	return groups
}

// galleryStats returns the catalog size and how many entries have no fixture
// (their authored render is empty or panics).
func galleryStats(entries []GalleryEntry) (total, needsFixture int) {
	total = len(entries)
	for _, e := range entries {
		if e.NeedsFixture() {
			needsFixture++
		}
	}
	return total, needsFixture
}

// galleryDependencyCount counts the dependency chips an entry will show, for
// the collapsed disclosure badge.
func galleryDependencyCount(entry GalleryEntry) int {
	return len(entry.Uses) + len(entry.UsedBy) + len(entry.ExternalDeps())
}

// galleryTransitiveUses returns the number of distinct components reachable
// through the entry's uses edges (the transitive closure, including the entry
// itself). It walks the raw graph with a visited set so render cycles terminate
// without topological sorting; it is the only consumer of walkGraph.
func galleryTransitiveUses(entry GalleryEntry) int {
	return len(walkGraph([]string{entry.ID}, graphUses))
}

// galleryLayerBlurb is the short layer description used by the index.
func galleryLayerBlurb(layer GalleryLayer) string {
	switch layer {
	case LayerDaisy:
		return "go-daisy primitives"
	case LayerAdapter:
		return "domain adapters"
	case LayerComposite:
		return "app composites"
	default:
		return string(layer)
	}
}

// galleryLayerHint is the longer layer explanation shown beside the section
// heading.
func galleryLayerHint(layer GalleryLayer) string {
	switch layer {
	case LayerDaisy:
		return "Primitives from go-daisy, added on demand when a cataloged component renders them."
	case LayerAdapter:
		return "Domain adapters that map app state to intent without extra markup."
	case LayerComposite:
		return "Reusable app composites — markup only, no domain, route, or config."
	default:
		return ""
	}
}
