package appletools

import "github.com/emergent-company/memory.web-ui/connector/internal/toolreg"

// PlatformNote explains why the Apple tool set is absent on hosts without
// osascript (non-macOS). `status` surfaces it when no Apple tools registered.
const PlatformNote = "Notes/Reminders tools unavailable: they run AppleScript via osascript, which is only present on macOS"

// Provider builds the Apple Notes/Reminders tool set backed by r. When the
// runner is unavailable it returns an empty tool set plus PlatformNote. No
// build tags: availability is detected at runtime.
func Provider(r Runner) ([]toolreg.Tool, string) {
	if !r.Available() {
		return nil, PlatformNote
	}
	return []toolreg.Tool{
		newNotesSearchTool(r),
		newNotesCreateTool(r),
		newRemindersListTool(r),
		newRemindersListsTool(r),
		newRemindersAddTool(r),
		newRemindersUpdateTool(),
		newRemindersDeleteTool(),
	}, ""
}

// DefaultProvider returns the tool set (and platform note) for the real
// osascript runner. Sections that wire the relay consume this.
func DefaultProvider() ([]toolreg.Tool, string) {
	return Provider(OSAppleScriptRunner{})
}
