## 1. Remove Bash Script

- [ ] 1.1 Delete `deploy/minimal/emergent-ctl.sh`

## 2. Update Installer

- [ ] 2.1 Edit `deploy/minimal/install-online.sh` line 406: remove `emergent-ctl.sh:emergent-ctl` from `BIN_FILES` array

## 3. Update Script References

- [ ] 3.1 Update `deploy/minimal/emergent-auth.sh` line 49: change `emergent-ctl restart` to `emergent-cli ctl restart`
- [ ] 3.2 Update `deploy/minimal/emergent-auth.sh` lines 235-236: change references from `emergent-ctl` to `emergent-cli ctl`

## 4. Update Documentation

- [ ] 4.1 Search all markdown files for `emergent-ctl` references using grep
- [ ] 4.2 Replace `emergent-ctl` with `emergent-cli ctl` in README files
- [ ] 4.3 Replace `emergent-ctl` with `emergent-cli ctl` in documentation under `docs/`
- [ ] 4.4 Check for `emergent-ctl` references in code comments

## 5. Verify and Test

- [ ] 5.1 Verify `emergent-cli ctl start` works with test installation
- [ ] 5.2 Verify `emergent-cli ctl stop` works with test installation
- [ ] 5.3 Verify `emergent-cli ctl logs` works with test installation
- [ ] 5.4 Verify `emergent-cli ctl health` works with test installation
- [ ] 5.5 Run installer script and confirm it doesn't attempt to download `emergent-ctl.sh`
- [ ] 5.6 Verify `emergent-auth.sh` script still functions correctly with updated commands

## 6. Release Communication

- [ ] 6.1 Add breaking change notice to CHANGELOG or release notes
- [ ] 6.2 Include migration examples (old command → new command)
- [ ] 6.3 Document that all `emergent-ctl` functionality is preserved in `emergent-cli ctl`
