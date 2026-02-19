# Firecracker Networking Setup for mcj-emergent

## Required iptables Rules

The following iptables rules are **required** for Firecracker VM networking on mcj-emergent.  
These rules must be applied **after every reboot** as they do not persist by default.

### 1. NAT POSTROUTING (Masquerade outbound traffic)

```bash
# Add NAT masquerading for Firecracker VM subnet (172.16.0.0/16)
# This MUST be at the TOP of the POSTROUTING chain (before ts-postrouting)
iptables -t nat -I POSTROUTING -s 172.16.0.0/16 -o eth0 -j MASQUERADE
```

**Explanation:** Firecracker VMs use `172.16.0.0/16` subnet. Outbound traffic must be SNAT'd  
to the host's IP (10.10.10.210) so return packets can be routed back. The `-o eth0` ensures  
we only masquerade traffic going out the host's main interface.

### 2. FORWARD Rules (Allow Firecracker TAP device traffic)

```bash
# Allow outbound traffic from Firecracker VMs
iptables -I FORWARD -i fctap+ -j ACCEPT

# Allow inbound traffic to Firecracker VMs
iptables -I FORWARD -o fctap+ -j ACCEPT

# Allow return traffic (connection tracking)
iptables -I FORWARD -i eth0 -o fctap+ -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
```

**Explanation:** mcj-emergent has `FORWARD` policy `DROP`, so we must explicitly allow:

- Packets **from** Firecracker TAP devices (`fctap+`) to anywhere
- Packets **to** Firecracker TAP devices from anywhere
- Return packets from established connections (RELATED,ESTABLISHED)

### 3. TCP MSS Clamping (Fix MTU issues)

```bash
# Clamp TCP MSS to PMTU to avoid fragmentation issues
iptables -t mangle -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
```

**Explanation:** Firecracker VMs may have MTU mismatches that cause TCP connections to hang.  
MSS clamping ensures TCP segments fit within the path MTU.

### 4. Docker DOCKER-USER Chain (Already configured)

```bash
# This should already be present (added by Docker)
iptables -A DOCKER-USER -j RETURN
```

**Explanation:** Docker's DOCKER-USER chain must end with RETURN so our FORWARD rules work.  
This is already configured on mcj-emergent.

---

## Setup Script

Run this script after every reboot:

```bash
#!/bin/bash
# /root/setup-firecracker-iptables.sh

set -e

echo "Setting up iptables rules for Firecracker..."

# 1. NAT POSTROUTING
if ! iptables -t nat -C POSTROUTING -s 172.16.0.0/16 -o eth0 -j MASQUERADE 2>/dev/null; then
    iptables -t nat -I POSTROUTING -s 172.16.0.0/16 -o eth0 -j MASQUERADE
    echo "✓ Added NAT MASQUERADE rule"
else
    echo "✓ NAT MASQUERADE rule already exists"
fi

# 2. FORWARD rules
if ! iptables -C FORWARD -i fctap+ -j ACCEPT 2>/dev/null; then
    iptables -I FORWARD -i fctap+ -j ACCEPT
    echo "✓ Added FORWARD rule (fctap+ inbound)"
else
    echo "✓ FORWARD rule (fctap+ inbound) already exists"
fi

if ! iptables -C FORWARD -o fctap+ -j ACCEPT 2>/dev/null; then
    iptables -I FORWARD -o fctap+ -j ACCEPT
    echo "✓ Added FORWARD rule (fctap+ outbound)"
else
    echo "✓ FORWARD rule (fctap+ outbound) already exists"
fi

if ! iptables -C FORWARD -i eth0 -o fctap+ -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null; then
    iptables -I FORWARD -i eth0 -o fctap+ -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
    echo "✓ Added FORWARD rule (conntrack)"
else
    echo "✓ FORWARD rule (conntrack) already exists"
fi

# 3. TCP MSS clamping
if ! iptables -t mangle -C FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null; then
    iptables -t mangle -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
    echo "✓ Added TCP MSS clamping rule"
else
    echo "✓ TCP MSS clamping rule already exists"
fi

echo
echo "Firecracker iptables rules configured successfully!"
echo
echo "Current state:"
echo "- NAT POSTROUTING (first 3 rules):"
iptables -t nat -L POSTROUTING -n -v | head -6
echo
echo "- FORWARD (first 6 rules):"
iptables -L FORWARD -n -v | head -9
echo
echo "- TCP MSS clamping:"
iptables -t mangle -L FORWARD -n -v | grep TCPMSS
```

Save to `/root/setup-firecracker-iptables.sh` and run:

```bash
chmod +x /root/setup-firecracker-iptables.sh
/root/setup-firecracker-iptables.sh
```

---

## Verification

After applying the rules, verify with:

```bash
# Check NAT rule
iptables -t nat -L POSTROUTING -n -v | grep 172.16

# Check FORWARD rules
iptables -L FORWARD -n -v | grep fctap

# Check MSS clamping
iptables -t mangle -L FORWARD -n -v | grep TCPMSS

# Test VM connectivity (if a VM is running)
curl http://172.16.X.Y:8080/health
```

---

## Summary of Changes Made

1. **Kernel upgraded** from 4.14.174 → 6.1.102 (supports virtio-rng, modern kernel features)
2. **DNS configuration** added to `/sbin/init` in rootfs (writes `/etc/resolv.conf` at boot)
3. **Entropy device** enabled via Firecracker API (`PUT /entropy`)
4. **iptables rules** configured for NAT and forwarding

**Result:** Firecracker VMs can now:

- ✅ Access entropy (256+ bits, `/dev/hwrng` exists)
- ✅ Resolve DNS (8.8.8.8, 8.8.4.4)
- ✅ Make HTTPS connections (`wget`, `curl`, `git clone`)
- ✅ Communicate with host and internet

---

## Next Steps

1. **Persist iptables rules** — Add `/root/setup-firecracker-iptables.sh` to systemd or `/etc/rc.local`
2. **Test end-to-end workspace creation** via Emergent API
3. **Deploy updated server** with `firecracker_provider.go` changes (entropy device support)
