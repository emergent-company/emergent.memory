## ADDED Requirements

### Requirement: Invitation delivery log on the members list

For every sent invitation on the project members surface, the Web UI SHALL make the invitation's email delivery log reachable from the invitation row without leaving the page: on pointer hover, and for keyboard users on activation of a focusable trigger. The log SHALL be rendered server-side into the existing popover component — not a hand-rolled tooltip — and SHALL show, per send, the send's state, the date and time the send completed (falling back to its enqueue time when it has not finished processing), and each Mailgun delivery event with any bounce/complaint reason. Sends SHALL be listed newest first. When an invitation has no sends, the log SHALL show a neutral empty state. The log SHALL NOT replace or remove the invitation's existing lifecycle and delivery badges, and SHALL NOT change its existing row actions (resend/revoke). The trigger SHALL be keyboard reachable and the popover SHALL be dismissible (Escape).

#### Scenario: Hover reveals the delivery log
- **WHEN** the operator hovers the invitation row's identity area
- **THEN** a popover shows that invitation's email send history

#### Scenario: Log shows each send with date/time and bounce detail
- **WHEN** a sent invitation has multiple sends and a bounce event carrying a reason
- **THEN** the log lists each send with its state and date/time, and the bounce event with its reason

#### Scenario: Sent date is the completion time
- **WHEN** a send has a processed timestamp (Mailgun accepted it) distinct from its enqueue time
- **THEN** the log shows the completion time, not the earlier enqueue time

#### Scenario: Keyboard reachable
- **WHEN** a keyboard user focuses the invitation row's trigger and activates it
- **THEN** the same delivery log popover is shown

#### Scenario: No sends yet
- **WHEN** an invitation has no email sends
- **THEN** the log shows a neutral "no emails sent" state

#### Scenario: Existing badges and actions unchanged
- **WHEN** the delivery log is added to an invitation row
- **THEN** the row still shows its lifecycle/delivery badges and its resend/revoke actions exactly as before
