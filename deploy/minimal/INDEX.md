# Emergent Standalone Deployment - Documentation Index

Complete guide to deploying and using Emergent in standalone mode with embedded CLI.

## Quick Links

| Document                                             | Purpose                          | Read Time |
| ---------------------------------------------------- | -------------------------------- | --------- |
| [INSTALL.md](./INSTALL.md)                           | **One-command installation**     | 3 min     |
| [README.md](./README.md)                             | Main deployment guide            | 15 min    |
| [CLI_QUICK_REFERENCE.md](./CLI_QUICK_REFERENCE.md)   | Common CLI commands              | 2 min     |
| [CLI_USAGE.md](./CLI_USAGE.md)                       | Complete CLI guide               | 10 min    |
| [CLI_EMBEDDED_SUMMARY.md](./CLI_EMBEDDED_SUMMARY.md) | Technical implementation details | 8 min     |
| [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md)       | Full deployment session report   | 20 min    |

## Getting Started (1 Minute)

**Just copy and paste this:**

```bash
curl -fsSL https://raw.githubusercontent.com/Emergent-Comapny/emergent/main/deploy/minimal/install.sh | bash
```

Done! See [INSTALL.md](./INSTALL.md) for details.

## For New Users

**First-time deployment?** Follow this path:

1. ✅ [README.md](./README.md) - Prerequisites, installation, basic configuration
2. ✅ [CLI_QUICK_REFERENCE.md](./CLI_QUICK_REFERENCE.md) - Essential commands
3. ✅ [CLI_USAGE.md](./CLI_USAGE.md) - When you need more detail

## For Operators

**Managing existing deployment?** Start here:

1. ✅ [CLI_QUICK_REFERENCE.md](./CLI_QUICK_REFERENCE.md) - Daily operations
2. ✅ [README.md](./README.md) - Troubleshooting section
3. ✅ [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md) - Deep dive when needed

## For Developers

**Contributing or customizing?** Read these:

1. ✅ [CLI_EMBEDDED_SUMMARY.md](./CLI_EMBEDDED_SUMMARY.md) - Architecture details
2. ✅ [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md) - Complete technical analysis
3. ✅ Build script: `build-server-with-cli.sh`
4. ✅ Dockerfile: `Dockerfile.server-with-cli`

## Document Purposes

### README.md (Main Guide)

- Deployment prerequisites
- Quick start instructions
- Environment configuration
- Service architecture
- Accessing deployment (Tailscale, localhost)
- Management commands
- Troubleshooting
- Security considerations

### CLI_QUICK_REFERENCE.md (Cheat Sheet)

- Most common CLI commands
- Basic usage patterns
- Output format options
- Scripting examples
- Quick troubleshooting

### CLI_USAGE.md (Complete CLI Guide)

- Detailed CLI usage in Docker
- Configuration options
- All available commands
- Docker Compose integration
- Automation examples
- Best practices
- Full automation workflow

### CLI_EMBEDDED_SUMMARY.md (Implementation)

- Technical architecture
- What was added (files, changes)
- Build process details
- Usage examples
- Migration guide
- Testing procedures
- Future enhancements

### DEPLOYMENT_REPORT.md (Session Report)

- Complete deployment session log
- Problem investigation
- Solution implementation
- Test results
- Known issues
- Recommendations

## Use Cases

### "I want to deploy Emergent quickly"

→ [README.md](./README.md) Quick Start → [CLI_QUICK_REFERENCE.md](./CLI_QUICK_REFERENCE.md)

### "I need to run CLI commands"

→ [CLI_QUICK_REFERENCE.md](./CLI_QUICK_REFERENCE.md) → [CLI_USAGE.md](./CLI_USAGE.md) for details

### "I'm troubleshooting an issue"

→ [README.md](./README.md) Troubleshooting → [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md) for deep dive

### "I want to automate operations"

→ [CLI_USAGE.md](./CLI_USAGE.md) Automation section → Example scripts

### "I need to understand the architecture"

→ [CLI_EMBEDDED_SUMMARY.md](./CLI_EMBEDDED_SUMMARY.md) → [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md)

### "I'm building custom Docker images"

→ [CLI_EMBEDDED_SUMMARY.md](./CLI_EMBEDDED_SUMMARY.md) → `build-server-with-cli.sh` → `Dockerfile.server-with-cli`

## File Structure

```
deploy/minimal/
├── README.md                          # Main deployment guide
├── CLI_QUICK_REFERENCE.md            # Command cheat sheet
├── CLI_USAGE.md                      # Complete CLI guide
├── CLI_EMBEDDED_SUMMARY.md           # Implementation details
├── DEPLOYMENT_REPORT.md              # Session report
├── INDEX.md                          # This file
├── docker-compose.local.yml          # Docker Compose config
├── Dockerfile.server-with-cli        # Enhanced Dockerfile
├── build-server-with-cli.sh          # Build automation
└── .env.example                      # Environment template
```

## Quick Command Reference

### Build Image

```bash
./build-server-with-cli.sh
```

### Start Deployment

```bash
docker-compose up -d
```

### Run CLI Command

```bash
docker exec emergent-server emergent-cli <command>
```

### Open Interactive Shell

```bash
docker exec -it emergent-server sh
```

### Check Status

```bash
docker exec emergent-server emergent-cli status
```

### View Logs

```bash
docker logs emergent-server
```

## Support Resources

- **CLI Source**: `/root/emergent/tools/emergent-cli/`
- **Server Source**: `/root/emergent/apps/server-go/`
- **GitHub Issues**: https://github.com/Emergent-Comapny/emergent/issues
- **Main Docs**: `/root/emergent/docs/`

## Update History

- **2026-02-06**: Initial embedded CLI implementation
  - Added Dockerfile.server-with-cli
  - Created build automation script
  - Updated docker-compose configuration
  - Wrote complete documentation suite

## Next Steps

After reviewing this index:

1. **First Deployment**: Start with [README.md](./README.md)
2. **Daily Use**: Bookmark [CLI_QUICK_REFERENCE.md](./CLI_QUICK_REFERENCE.md)
3. **Deep Dive**: Read [CLI_EMBEDDED_SUMMARY.md](./CLI_EMBEDDED_SUMMARY.md)
4. **Troubleshooting**: Reference [DEPLOYMENT_REPORT.md](./DEPLOYMENT_REPORT.md)

---

**Last Updated**: February 6, 2026  
**Deployment Version**: Standalone with Embedded CLI  
**Status**: ✅ Production Ready
