package main

import "encoding/json"

// agentVisibilityDropdownData returns the Alpine x-data object literal for the
// custom Visibility listbox in the agent General settings.
//
// A native <select>/<option> control cannot render a second, dimmed description
// line per option, so the control is a custom listbox: a hidden input carries
// the committed value for form submission, and Alpine owns opening the panel,
// roving-tabindex keyboard navigation, and committing a choice back into that
// hidden input. The option values and the trigger's value→label map are derived
// from agentVisibilityOptions so the copy lives in exactly one place.
func agentVisibilityDropdownData(selected string) string {
	values := make([]string, 0, len(agentVisibilityOptions))
	labels := make(map[string]string, len(agentVisibilityOptions))
	for _, opt := range agentVisibilityOptions {
		values = append(values, opt.Value)
		labels[opt.Value] = opt.Label
	}
	nv, ok := agentVisibilityNormalize(selected)
	if !ok {
		nv = agentVisibilityProject
	}
	vj, _ := json.Marshal(values)
	lj, _ := json.Marshal(labels)
	sj, _ := json.Marshal(nv)
	return `{ open: false, value: ` + string(sj) + `, active: 0, options: ` + string(vj) +
		`, labels: ` + string(lj) +
		`, toggle() { this.open ? this.close() : this.openList(); }` +
		`, openList() { var i = this.options.indexOf(this.value); this.active = i < 0 ? 0 : i; this.open = true; this.$nextTick(() => this.focusActive()); }` +
		`, close() { this.open = false; }` +
		`, closeAndFocus() { this.open = false; this.$nextTick(() => { if (this.$refs.trigger) this.$refs.trigger.focus(); }); }` +
		`, choose(v) { if (this.options.indexOf(v) < 0) return; this.value = v; this.closeAndFocus(); }` +
		`, move(d) { var n = this.options.length; this.active = (this.active + d + n) % n; this.focusActive(); }` +
		`, setActive(i) { this.active = i; }` +
		`, focusActive() { var el = this.$refs.list && this.$refs.list.querySelector('[data-index="' + this.active + '"]'); if (el) el.focus(); } }`
}
