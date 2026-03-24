# K108: Sovereign Triggers - Project GitSovereign

This document defines the technical and business thresholds for transitioning GitSovereign through its maturity stages.

## Trigger A: Expansion (Parallel Density)
- **Goal**: Move from Single-Repo to Full-Organization Parallel Harvests.
- **Thresholds**:
    - **Technical**: Sustained egress throughput > 50MB/s and GitHub API quota usage < 40% over a 1-hour window.
    - **Business**: Acquiring 5+ organization-level customers with > 50 repositories each.
- **Action**: Activate the **Parallel-First Concurrency Orchestrator** (Pulse 2) for multi-threaded QUIC streams across the entire organization.

## Trigger B: Compliance (Automated Auditing)
- **Goal**: Transition to fully automated SOC2/HIPAA assurance reporting.
- **Thresholds**:
    - **Value Unit Volume**: Total `AssuranceReportUnit` (from Pulse 6) volume exceeding 1,000 units/month.
    - **Customer Signal**: > 30% of the active revenue base requiring verifiable "Availability" evidence for external auditors.
- **Action**: Enable the **Pinnacle-Assurance Auto-Validator** and integrate `.jebnf` audit logs into the customer dashboard.

## Trigger C: Pivot (Federated Storage Mesh)
- **Goal**: Transition from GCS/Drive to the **Federated Storage Mesh**.
- **Thresholds**:
    - **Data Volume**: Organization-wide "Sovereign Tree" size > 100TB across all cloud destinations.
    - **Latency Limit**: RTO (Recovery Time Objective) for sub-second rehydration exceeding 1s due to cloud provider egress bottlenecks.
- **Action**: Initiate the **Firehorse Mesh** (Pulse 8/9 opportunity) to distribute backup blocks across a federated network of sovereign workstations.
