## Purpose

Define how the Web UI presents the delivery state of a sent invitation and how an org admin resends one from the project members surface.

## ADDED Requirements

### Requirement: Delivery state badge on sent invitations

The Web UI sent-invitations list SHALL display, for each sent invitation, an email delivery badge derived from the invitation's `deliveryStatus` returned by the invitations API. The badge SHALL distinguish at least: no delivery event yet (sent), delivered, opened, clicked, and a problem state (bounced, soft-bounced, complained, or failed). A missing (null) `deliveryStatus` SHALL render as a neutral "sent" state rather than an error. The badge SHALL use the shared status-badge component and the daisyUI soft/style tokens used elsewhere, and SHALL NOT replace the invitation lifecycle status.

#### Scenario: Delivered invitation shows a delivered badge
- **WHEN** a sent invitation has `deliveryStatus = 'delivered'`
- **THEN** its row shows a delivered delivery badge

#### Scenario: No events yet shows a neutral badge
- **WHEN** a sent invitation has a null `deliveryStatus`
- **THEN** its row shows a neutral "sent" badge and no error state

#### Scenario: Bounce shows a problem badge
- **WHEN** a sent invitation has `deliveryStatus = 'bounced'`, `'soft_bounced'`, `'complained'`, or `'failed'`
- **THEN** its row shows a problem-state delivery badge

### Requirement: Resend action on pending invitations

For every pending sent invitation, the Web UI SHALL offer a resend action that submits `POST /invites/:id/resend`. The action SHALL be gated the same way as revoke (visible only to a caller who may administer the organization). On success the UI SHALL return the caller to the members surface they came from and reflect that a resend occurred; the invitation SHALL remain listed as pending. Resend SHALL NOT be offered for invitations that are not pending.

#### Scenario: Resend submits and returns to the same surface
- **WHEN** an org admin activates resend on a pending invitation from the project members page
- **THEN** the browser submits `POST /invites/:id/resend` and is returned to the members surface it came from

#### Scenario: Resend is not offered for non-pending invitations
- **WHEN** an invitation is accepted, declined, or revoked
- **THEN** no resend control is shown for it

#### Scenario: Resend hidden from non-admins
- **WHEN** the caller may not administer the organization
- **THEN** no resend control is shown
