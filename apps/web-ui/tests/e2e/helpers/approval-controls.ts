/**
 * The A2UI approval card's decision set — one owner, two consumers.
 *
 * `chat-components.js a2uiApproval` mirrors the canonical chat approval card
 * (`chat-stream.js renderApproval`) and renders exactly these controls, with
 * these hook testids and visible labels. Two specs assert them:
 *
 *  - `specs/js/a2ui-approvals-gate.spec.ts` runs the shipped renderer
 *    hermetically and is the OWNER: it is where a renderer change first breaks.
 *  - `specs/chat/a2ui-widgets-ui.spec.ts` drives the same renderer through a
 *    real gateway turn (session mode, scheduled lane).
 *
 * Deriving both from this constant is what keeps the scheduled spec from naming
 * a control the renderer does not have — the #584 failure, where that spec
 * asserted a "Deny" action that has never existed. What it does NOT do is notice
 * a *new* inline control name typed into some other spec; that needs a lint the
 * repo does not have, so it stays a review concern.
 *
 * Pure data: no Playwright, no `node:fs`, so a hermetic (`js-dom`) spec can
 * import it without pulling the session-mode helpers in.
 */
export const APPROVAL_DECISIONS = [
  { testid: 'chat-approval-cancel', label: 'Cancel' },
  { testid: 'chat-approval-reject', label: 'Reject' },
  { testid: 'chat-approval-approve', label: 'Approve' },
] as const;
