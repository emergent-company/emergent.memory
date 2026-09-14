# Password-field toggle affordance

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-password-reveal-toggle](sessions/2026-09-09-password-reveal-toggle.md)

## What

GoDAISY `form.PasswordField`'s show/hide toggle is now a transparent icon overlaid inside the input's end edge (go-daisy `47e3762`). Consider whether it needs a stronger affordance — e.g. a hover/active circle background, a slightly larger hit target, or a `title` tooltip — and apply it in the component if the user wants it.

## Why

The icon currently signals clickability only by a color shift (`text-base-content/40` → full on hover). A native select's chevron is passive, but this control is interactive, so discoverability/affordance is a genuine open question. User was offered this follow-up; preference not yet stated.

## Depends on

none

## Notes

- Component lives in `/root/go-daisy` (`components/form/password_shared.templ`), not the gateway.
- Keep it a component change so all consumers inherit it; any new utility classes need a gateway `task css` regen after the bump.
- Any hover-background approach must not reintroduce a visible "button tile" seam inside the field.
