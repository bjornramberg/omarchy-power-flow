# TODO

Only intentionally deferred work belongs here.

## Optional one-time power calibration

The UTH Power application itself must always run unprivileged. Add an optional,
explicit calibration workflow that users may run separately with elevated
access:

- Capture CPU package RAPL readings at idle and several utilization levels.
- Derive machine-specific CPU idle, load, and peak coefficients.
- Store the calibration profile locally without granting runtime privileges.
- Use the calibrated profile in the unprivileged collector, with the generic
  model as fallback.
- Show calibration age, confidence, and provenance in the panel.
- Never invoke privilege escalation automatically.
- Treat RAM separately: this AMD platform exposes no DRAM RAPL domain, so RAM
  calibration must remain conservative unless another measurable reference is
  available.
