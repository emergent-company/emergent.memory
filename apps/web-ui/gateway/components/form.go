package components

// formActionsAlign maps a form-action row's Align value onto the flex
// justify utility. The zero value right-aligns: main actions belong on the
// right of their row, which is the house pattern and the default in the
// go-daisy form package's own footer. Both footer components
// (InlineFormActions for in-form rows, StickyFormActions for page-terminal
// strips) share this mapping so the two can never drift apart.
func formActionsAlign(align string) string {
	switch align {
	case "start":
		return "justify-start"
	case "between":
		return "justify-between"
	default:
		return "justify-end"
	}
}
