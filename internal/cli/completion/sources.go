package completion

// sources is what each kind answers with.
//
// A kind with no entry completes nothing, which is what a slot declared but
// not yet implemented does: the declaration is the contract, and the source
// behind it can arrive later without anything else changing.
//
// Each source registers itself from the file it lives in rather than being
// listed here, so two of them can be written at once without touching the same
// line.
var sources = map[Kind]Source{}

func register(kind Kind, source Source) {
	sources[kind] = source
}
