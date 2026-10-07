# Schema-2 prototype: failed driver attempt

The generator, CLI scaffold/check and native project tests passed. The first
independent-wire comparison stopped on two zero-account rows: the Go driver's
nil result slice encoded as JSON `null`, while the independent expectation was
an empty JSON array. Error codes and all nonempty state vectors agreed.

No SBF build or runtime suite executed in this attempt. Its `passed:false`
summary and command logs are preserved. The driver now initializes an empty
result slice; a new result directory records subsequent validation.
