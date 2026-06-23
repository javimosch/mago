---
name: loop-cadence-tests
description: loop.go cadence is testable via 3 extracted helpers: parseLoopArgs (flags w/ def…
---
## Notes

- 2026-06-23T13-11-34Z [cto] loop.go cadence is testable via 3 extracted helpers: parseLoopArgs (flags w/ defaults 3/60/5), nextInterval (reset to base on error-free work else double+clamp to maxI), and driveLoop (loop with injectable run/sleep, no sleep after final tick, returns ticks run). Tests need no live company or real time.
