# Phased implementation plans

This directory sequences the work for the machine package and the flow
package into small phases. Each phase is one pass through the delivery
loop: plan review, build, implementation review, verify, and commit.

The package plans remain the gate-mandated contracts. See
docs/plans/machine.md and docs/plans/flow.md. The phase files here
break that work into shippable steps.

## Build order

| Phase | Package | Delivers | Depends on |
|---|---|---|---|
| 1 | machine | types, Definition, Validate | none |
| 2 | machine | Fire engine | phase 1 |
| 3 | machine | wire form, vectors | phase 2 |
| 4 | machine | benchmarks | phase 3 |
| 5 | flow | step graph, Kahn validation | phase 1 |
| 6 | flow | runner, parallel panels | phase 5 |
| 7 | flow | chaining | phase 6 |
| 8 | flow | end-to-end, concurrency, benchmarks | phase 7 |

Build machine before flow. The flow package imports machine. Phase 5
starts in parallel with phase 4 because it only needs phase 1.

## Test coverage

Every phase follows test-driven development. Write the failing test,
then the code, then the passing test.

Integration tests appear in phases 3, 6, 7, and 8. They cross
package boundaries and run real flows. Performance tests appear in
phases 4 and 8. They use the bench target in the Makefile.
