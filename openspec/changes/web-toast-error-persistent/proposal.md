## Why

Error feedback currently auto-dismisses like every other toast, so a failed
action's message can vanish before the user reads it. There is also no explicit
dismiss affordance — the whole toast is a click target, which is easy to trigger
accidentally and cannot be relied on for a toast that must stay put.

## What Changes

- Error toasts become **persistent**: they do not auto-dismiss and render an
  explicit close (dismiss) control. `success`/`info`/`warning` keep
  auto-dismissing and click-anywhere dismissal.
- Server PRG flash feedback and client-originated toasts share one
  severity→duration rule (`error` = persistent/0, otherwise the shared short
  timeout).
- The go-daisy toast queue gains `duration <= 0` persistent support and a close
  button (upstream go-daisy change; the gateway bumps its pin).

## Capabilities

### New Capabilities

- `web-toast-feedback`: toast severity semantics — persistent, dismissible errors
  vs auto-dismissing success/info — shared by server flash and client toasts.

### Modified Capabilities

<!-- none -->

## Impact

- `apps/web-ui/gateway/ui.go` — `flashToast` sends a per-kind duration.
- `apps/web-ui/gateway/webui/static/js/app.js` — client `toast()` applies the same rule.
- `apps/web-ui/gateway/go.mod` — bumped go-daisy pin (persistent toast + close button).
- `apps/web-ui/docs/ui-patterns.md`, `apps/web-ui/docs/ui-recipes.md` — documented pattern.
