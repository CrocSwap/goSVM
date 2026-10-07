# Clock bring-up attempt

The SDK accessor passed native/C checks and the first SBF instruction. The
stateful fixture then repeated identical instruction bytes without a fresh
blockhash, causing `AlreadyProcessed`. Changing a sysvar does not change the
transaction signature. The failed [summary](summary.json) and logs are preserved.
The subsequent experiment marks every repeated sequence transaction with a fresh
blockhash; no runtime assertions or checks were weakened.
