## 1. Update emergent-onboard SKILL.md

- [x] 1.1 Renumber existing Step 2 (design pack) → Step 3, Step 3 (install pack) → Step 4, Step 4 (populate) → Step 5
- [x] 1.2 Insert new Step 2 "Choose or create an Emergent project" with: check `.env.local` for existing `EMERGENT_PROJECT`; if found, confirm or offer to switch; if not found, run `emergent projects list` and present options
- [x] 1.3 Add `.env.local` write instructions: create/append/replace `EMERGENT_PROJECT=<id>` depending on file state
- [x] 1.4 Add note to remind user to add `.env.local` to `.gitignore` if not already present
- [x] 1.5 Update the "already onboarded" note at the bottom to reference `.env.local` detection logic

## 2. Update embedded skill in CLI

- [x] 2.1 Rebuild CLI binary with updated SKILL.md: `go build -o ~/.local/bin/emergent ./cmd/`
- [x] 2.2 Reinstall skills in test repo (meta-project): `cd ~/meta-project && emergent skills install --force`
- [x] 2.3 Verify `emergent skills list` shows updated skill with correct description
