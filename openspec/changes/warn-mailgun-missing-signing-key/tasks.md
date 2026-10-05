## 1. Startup warning

- [x] 1.1 Add a `log.Warn` in `NewSender`'s Mailgun branch when `cfg.MailgunSigningKey == ""`, after the existing `log.Info`. Verify: `go build ./...` compiles.
- [x] 1.2 Add a focused test asserting the warning is emitted when the signing key is empty and absent when it is set. Verify: `go test ./domain/email/...` passes.

## 2. Docs

- [x] 2.1 Add `MAILGUN_SIGNING_KEY` to the server env-var config table.
- [x] 2.2 Rewrite the "Delivery tracking" section to describe the signed public webhook.

## 3. Spec

- [x] 3.1 Create the OpenSpec change with an ADDED requirement + scenario.

## 4. Verification

- [x] 4.1 `go build ./...`, `go test ./domain/email/...`, `gofmt -l .`, and `openspec validate` all pass.
