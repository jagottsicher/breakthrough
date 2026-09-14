# go-callvis determinism patch

`go-callvis-determinism.patch` fixes two spots in `github.com/ofabry/go-callvis`
where the tool builds its `.dot` output from Go maps without sorting first:

- `callgraph.GraphVisitEdges` (in `golang.org/x/tools/go/callgraph`) walks
  `callgraph.Graph.Nodes`, a `map[*ssa.Function]*Node`, to seed its traversal.
  Map iteration order is randomized per process, so which node/edge is
  discovered first -- and therefore the order nodes and edges end up in the
  generated `.dot` text -- varies between two otherwise identical runs.
- `dotAttrs.List()` renders a node/edge/cluster's attributes by ranging over
  `dotAttrs`, itself a `map[string]string`, with the same problem.

Either one on its own is enough to make the rendered SVG (different node
IDs, different edge IDs, different `dot` layout coordinates) differ from run
to run for the exact same source tree and the exact same go-callvis version,
which is what the `Verify committed callgraph` step in
`.github/workflows/callgraph.yml` compares against. The patch sorts the
traversal's own node/edge slices (recursing into the per-package
sub-clusters the default `-group=pkg` behavior creates) and the rendered
attribute list, both by a stable string key, so a fixed source tree always
produces byte-identical output.

Regenerate this patch by re-running the same two edits against a fresh
`git clone https://github.com/ofabry/go-callvis.git` and taking
`git diff -- dot.go output.go`, should upstream ever change enough that it
stops applying cleanly.
