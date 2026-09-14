// memory-reminders: EventKit-backed reminders CLI for the Memory connector.
//
// Why a native helper: the connector's osascript/JXA reminders path is unusable
// on large libraries (the scripting bridge costs ~2s per app query and
// per-item property access round-trips). EventKit is the supported, fast API.
//
// Contract (stdout is JSON only; stderr is a human-readable error; non-zero
// exit on failure):
//   list   [--list NAME] [--include-completed] [--include-notes]
//          -> [{"id","name","list","due_date","completed"[,"notes"]}, ...]
//   lists  -> [{"id","name","count"}, ...]
//   add    --title T [--list NAME] [--due RFC3339] [--notes N]
//          -> canonical reminder object
//   update --id ID [--title T] [--due RFC3339 | --clear-due]
//                 [--notes N | --clear-notes] [--priority N]
//                 [--completed true|false] [--list NAME]
//          -> canonical reminder object
//   delete --id ID
//          -> {"id":"...","deleted":true}
//
// Pure parsing/encoding/RFC3339 helpers live in RemindersHelperCore.swift so
// the unit-test target can exercise them without an event store.
//
// TCC: this binary is embedded in Memory.app/Contents/Resources and is spawned
// only as a direct child of the app's engine, so the Reminders permission
// prompt/attribution belongs to Memory.app (Info.plist
// NSRemindersFullAccessUsageDescription).

import Dispatch
import EventKit
import Foundation

// Box synchronizes a value written from an EventKit completion handler and read
// after a semaphore wait (the semaphore provides the ordering; the reference
// box keeps Swift's concurrency diagnostics quiet).
final class Box<T> {
    var value: T
    init(_ value: T) { self.value = value }
}

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data(("error: " + message + "\n").utf8))
    exit(1)
}

let rfc3339 = RFC3339()

let parsed: ParsedArguments
do {
    parsed = try parseArguments(Array(CommandLine.arguments.dropFirst()))
} catch let error as HelperError {
    fail(error.description)
} catch {
    fail("could not parse arguments: \(error.localizedDescription)")
}

// ---- EventKit access ----------------------------------------------------

let store = EKEventStore()

func verifyAccess() {
    let status = EKEventStore.authorizationStatus(for: .reminder)
    switch status {
    case .authorized, .fullAccess:
        return
    case .denied:
        fail("Reminders access denied. Grant Memory access in System Settings → Privacy & Security → Reminders.")
    case .restricted:
        fail("Reminders access is restricted on this Mac (parental controls or MDM).")
    case .notDetermined:
        break
    case .writeOnly:
        fail("Memory has only write-only Reminders access; full access is required to manage reminders.")
    @unknown default:
        break
    }

    let result = Box<(granted: Bool, error: Error?)>((false, nil))
    let semaphore = DispatchSemaphore(value: 0)
    store.requestFullAccessToReminders { granted, error in
        result.value = (granted, error)
        semaphore.signal()
    }
    if semaphore.wait(timeout: .now() + 60) == .timedOut {
        fail("timed out waiting for the Reminders access prompt")
    }
    if result.value.granted {
        return
    }
    if let error = result.value.error {
        fail("could not obtain Reminders access: \(error.localizedDescription)")
    }
    fail("Reminders access denied. Grant Memory access in System Settings → Privacy & Security → Reminders.")
}

verifyAccess()

// ---- shared helpers -----------------------------------------------------

func emit<T: Encodable>(_ value: T) {
    do {
        let data = try encodeJSON(value)
        FileHandle.standardOutput.write(data)
    } catch {
        fail("could not encode result as JSON: \(error.localizedDescription)")
    }
}

func fetchAllReminders() -> [EKReminder] {
    let reminders = Box<[EKReminder]>([])
    let semaphore = DispatchSemaphore(value: 0)
    _ = store.fetchReminders(matching: store.predicateForReminders(in: nil)) { fetched in
        reminders.value = fetched ?? []
        semaphore.signal()
    }
    if semaphore.wait(timeout: .now() + 60) == .timedOut {
        fail("timed out fetching reminders from EventKit")
    }
    return reminders.value
}

// resolveReminder maps a calendarItemIdentifier back to a live reminder. An
// unknown or non-reminder id is a hard error naming the id (design D2/D8).
func resolveReminder(id: String) -> EKReminder {
    guard let item = store.calendarItem(withIdentifier: id) else {
        fail("no reminder found with id '\(id)'")
    }
    guard let reminder = item as? EKReminder else {
        fail("no reminder found with id '\(id)' (calendar item is not a reminder)")
    }
    return reminder
}

// resolveList maps a list name to a modifiable reminder calendar, rejecting
// unknown, ambiguous, non-reminder, and read-only targets (design D6). Error
// wording deliberately contains "list" plus a not-found/invalid phrase so the
// connector's wrapReminderMutationError maps it to ErrInvalidTarget.
func resolveList(name: String) -> EKCalendar {
    let matches = store.calendars(for: EKEntityType.reminder).filter { $0.title == name }
    guard !matches.isEmpty else {
        fail("reminder list not found: '\(name)'")
    }
    guard matches.count == 1 else {
        fail("invalid target list: multiple reminder lists named '\(name)'")
    }
    let calendar = matches[0]
    guard calendar.allowedEntityTypes.contains(.reminder) else {
        fail("list '\(name)' does not accept reminders")
    }
    guard calendar.allowsContentModifications else {
        fail("list '\(name)' does not allow modifications (read-only)")
    }
    return calendar
}

// dueISOString renders the reminder's due date as RFC3339 UTC, honoring the
// time zone stored separately on the reminder (design D7).
func dueISOString(for reminder: EKReminder) -> String? {
    guard let components = reminder.dueDateComponents else { return nil }
    var calendar = components.calendar ?? Calendar.current
    if let timeZone = reminder.timeZone {
        calendar.timeZone = timeZone
    }
    guard let date = calendar.date(from: components) else { return nil }
    return rfc3339.string(from: date)
}

// applyDueDate builds dueDateComponents in the local calendar and stores the
// time zone on the reminder separately: embedding a time zone inside the
// components can be silently dropped by iCloud (design D7).
func applyDueDate(_ date: Date, to reminder: EKReminder) {
    var calendar = Calendar(identifier: .gregorian)
    let timeZone = TimeZone.current
    calendar.timeZone = timeZone
    let components = calendar.dateComponents(
        [.year, .month, .day, .hour, .minute, .second],
        from: date
    )
    reminder.dueDateComponents = components
    reminder.timeZone = timeZone
}

// reminderRow projects an EKReminder onto the JSON contract. emitNotes forces
// the notes key (even when null, for `list --include-notes`); mutations pass
// true only when the reminder actually has note text, so canonical write
// results carry notes "when present".
func reminderRow(for reminder: EKReminder, emitNotes: Bool) -> ReminderRow {
    ReminderRow(
        id: reminder.calendarItemIdentifier,
        name: reminder.title ?? "",
        list: reminder.calendar?.title ?? "",
        dueDate: dueISOString(for: reminder),
        completed: reminder.isCompleted,
        notes: reminder.notes,
        emitNotes: emitNotes
    )
}

// ---- subcommands --------------------------------------------------------

func runList(_ args: ParsedArguments) {
    let reminders = fetchAllReminders()
    var rows: [ReminderRow] = []
    rows.reserveCapacity(reminders.count)
    for reminder in reminders {
        if !args.includeCompleted && reminder.isCompleted {
            continue
        }
        if let listName = args.listName, reminder.calendar?.title != listName {
            continue
        }
        rows.append(reminderRow(for: reminder, emitNotes: args.includeNotes))
    }
    emit(rows)
}

func runLists() {
    let calendars = store.calendars(for: EKEntityType.reminder)
    let reminders = fetchAllReminders()
    var counts: [String: Int] = [:]
    for reminder in reminders {
        guard let identifier = reminder.calendar?.calendarIdentifier else { continue }
        counts[identifier, default: 0] += 1
    }
    let rows = calendars.map { calendar in
        ListRow(
            id: calendar.calendarIdentifier,
            name: calendar.title,
            count: counts[calendar.calendarIdentifier] ?? 0
        )
    }
    emit(rows)
}

func runAdd(_ args: ParsedArguments) {
    let reminder = EKReminder(eventStore: store)
    reminder.title = args.title ?? ""
    if let listName = args.listName {
        reminder.calendar = resolveList(name: listName)
    } else if let defaultCalendar = store.defaultCalendarForNewReminders() {
        reminder.calendar = defaultCalendar
    }
    if let due = args.due {
        applyDueDate(due, to: reminder)
    }
    if let notes = args.notes {
        reminder.notes = notes
    }
    do {
        try store.save(reminder, commit: true)
    } catch {
        fail("could not save reminder: \(error.localizedDescription)")
    }
    emit(reminderRow(for: reminder, emitNotes: reminder.notes != nil))
}

func runUpdate(_ args: ParsedArguments) {
    let reminder = resolveReminder(id: args.id ?? "")
    if let title = args.title {
        reminder.title = title
    }
    if let due = args.due {
        applyDueDate(due, to: reminder)
    }
    if args.clearDue {
        reminder.dueDateComponents = nil
        reminder.timeZone = nil
    }
    if let notes = args.notes {
        reminder.notes = notes
    }
    if args.clearNotes {
        reminder.notes = nil
    }
    if let priority = args.priority {
        reminder.priority = priority
    }
    if let completed = args.completed {
        reminder.isCompleted = completed
        if completed {
            if reminder.completionDate == nil {
                reminder.completionDate = Date()
            }
        } else {
            reminder.completionDate = nil
        }
    }
    if let listName = args.listName {
        reminder.calendar = resolveList(name: listName)
    }
    do {
        try store.save(reminder, commit: true)
    } catch {
        fail("could not save reminder: \(error.localizedDescription)")
    }
    // Re-read through the resolved id so a move/complete is reflected exactly.
    emit(reminderRow(for: reminder, emitNotes: reminder.notes != nil))
}

func runDelete(_ args: ParsedArguments) {
    let id = args.id ?? ""
    let reminder = resolveReminder(id: id)
    do {
        try store.remove(reminder, commit: true)
    } catch {
        fail("could not delete reminder '\(id)': \(error.localizedDescription)")
    }
    emit(DeletedResult(id: id, deleted: true))
}

switch parsed.command {
case .list:
    runList(parsed)
case .lists:
    runLists()
case .add:
    runAdd(parsed)
case .update:
    runUpdate(parsed)
case .delete:
    runDelete(parsed)
}
