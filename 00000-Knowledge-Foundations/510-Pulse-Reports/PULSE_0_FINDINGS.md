# Pulse 0: Market Discovery & Sovereign Requirements (US Commercial Findings)

## 1. Executive Summary
GitSovereign's "Firehorse" architecture directly addresses the **Availability** and **Confidentiality** criteria of SOC2 Type II, which are currently underserved by GitHub's native redundancy. For US-based commercial startups (Fintech, Healthtech, SaaS), this service is not just a backup—it is a **Revenue Enabler** that significantly improves RFP win rates and investor confidence.

## 2. Value Thresholds: The Cost of Silence
- **The $5,600/Minute Metric**: The industry average cost of IT downtime is $5,600 per minute. 
- **The Startup Multiplier**: For startups, downtime costs **3x more** than visible revenue loss due to churn, brand destruction, and distraction during critical growth phases.
- **Survival Rate**: 93% of companies experiencing prolonged downtime fail within 12 months.
- **Sub-Second Delta**: While a 1-hour RTO (Recovery Time Objective) might satisfy a basic audit, sub-second recovery ensures zero interruption for **High-Frequency DevOps** and automated deployment pipelines, preventing the "Sync Drift" that causes standard rehydration to fail.

## 3. SOC2 Type II Control Mapping
### Availability (TSC 3)
- **Mandate**: Systems must be accessible and operational as agreed.
- **Firehorse Alignment**: Automated, nightly Zero-Disk Harvests with verifiable Assurance Reports.
- **Granular Recovery**: Unlike full-system restores, Firehorse enables sub-second recovery of specific repository states (Unique Hash Nodes).

### Confidentiality (TSC 4)
- **Mandate**: Sensitive info must be protected from unauthorized disclosure.
- **Firehorse Alignment**: End-to-end encryption (AES-256) in transit and at rest.
- **Zero-Storage Lease**: RAM-only piping ensures that propriety code is never persisted on unencrypted interim disks during harvest.

## 4. US Commercial Segment Triggers
- **Solo/Small Team**: Triggered by "Fear of Platform Loss" and the need for simple, immediate monetization (the "Insurance" model).
- **Growth/Enterprise**: Triggered by the **Compliance Wall**. Startups hitting the Seed-to-Series A transition require automated "Availability Evidence" to close Enterprise accounts.

## 5. Competitive Gap
Existing BaaS (Rewind, GitProtect) are heavily disk-dependent and rely on standard HTTP/ConnectRPC layers. GitSovereign's **Raw QUIC + jeBNF** bypass provides a 10x performance delta, enabling the "Sub-Second" promise that competitors cannot mathematically reach over standard protocols.
