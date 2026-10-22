# testdata

`ole2-fixture.xls` is a copy of `test/test.xls` from
[richardlehane/mscfb](https://github.com/richardlehane/mscfb) v1.0.9
(Apache License 2.0), the same Compound File Binary reader this
package's own `ForOLE2` (ole2.go) depends on.

It exists here, rather than a hand-built minimal fixture the way
`ForPDF`/`ForImage`'s own tests construct theirs, because the
Compound File Binary format (sector table, FAT, directory stream) is
too involved to hand-construct validly in a test — unlike a minimal
PDF (plain text) or JPEG (a handful of well-documented markers). Using
the dependency's own small conformance fixture, already part of this
project's build graph, is a real DOC/XLS/PPT-shaped file without
adding a second format implementation just to generate a test input
for the first.
