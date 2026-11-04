package main

// pageFormChromeClass and pageFormFieldsClass are the hand-rolled wrappers that
// replace layout.Container(layout.ContainerLG, nil) on page-terminal form pages.
//
// A page-terminal form must be a DIRECT child of main#main-content (see
// components.StickyFormActions) so its sticky action bar can span the full
// content pane and sit flush with the pane's bottom edge at max scroll. That
// rules out wrapping the form in layout.Container, whose mx-auto p-6 lg:p-8
// max-w-6xl box would cap the bar's width and leave a 24-32px gap below it.
//
// The container's padding is split across two wrappers instead:
//   - pageFormChromeClass: the non-form page chrome (header, breadcrumbs,
//     toasts, type pickers) — top padding only, a direct child of main, before
//     the form.
//   - pageFormFieldsClass: the form's fields/cards/rail — bottom padding only,
//     inside the form, before the sticky bar.
//
// The bottom padding lives INSIDE the form so nothing sits below the bar: at
// max scroll the bar is the last in-flow child and rests on the pane bottom.
//
// These deliberately duplicate layout.ContainerLG's width/gutter (the page
// width rule) but cannot reuse layout.Container because the padding must be
// split. See TestPageWidthConsistency, which these constants satisfy since they
// live in a .go file, not a scanned .templ source.
const (
	pageFormChromeClass = "mx-auto w-full max-w-6xl px-6 pt-6 lg:px-8 lg:pt-8"
	pageFormFieldsClass = "mx-auto w-full max-w-6xl px-6 pb-6 lg:px-8 lg:pb-8"
)
