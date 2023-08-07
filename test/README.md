## Tests 

### `testdir/proc`
Wrangled some live proc data to test against. Should consider using filesystem mock in the future, but this is a quick and dirty way to run some tests. Should also consider pruning these for the future. For reference: 

- `1266999` -> is a process spawned by running `jupyter notebook &` (useful for testing interactive programs & python restores)
- `1227709` -> is a process spawned by running `./server -m models/7B/ggml-model-q4_0.bin -c 2048 &` (useful for testing servers & network restores)
